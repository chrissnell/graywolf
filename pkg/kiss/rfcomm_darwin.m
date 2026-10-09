//go:build darwin && cgo

// IOBluetooth RFCOMM (Bluetooth Classic SPP) client for macOS.
//
// macOS exposes a paired radio as /dev/cu.<name>, but that pseudo-serial
// port only dials the radio when macOS itself created the link. After the
// radio restarts and reconnects on its own, macOS handles it as an
// *incoming* connection, opening the cu.* node does not dial out, and the
// port is silently dead until the device is re-paired. This file talks to
// the radio's Serial Port service directly through IOBluetooth instead.
//
// IOBluetooth delivers delegate callbacks on the run loop of the thread
// that opened the channel, and Go has no run loop, so every IOBluetooth
// call is made on one dedicated NSThread that runs an NSRunLoop.

#import <Foundation/Foundation.h>
#import <IOBluetooth/IOBluetooth.h>

#include <errno.h>
#include <fcntl.h>
#include <string.h>
#include <unistd.h>

#include "rfcomm_darwin.h"

static NSThread *gBTThread;
static dispatch_once_t gBTOnce;
static NSMutableSet *gLive; // sessions kept alive until IOBluetooth is done with them

@interface GWBlockRunner : NSObject
+ (void)run:(void (^)(void))blk;
@end
@implementation GWBlockRunner
+ (void)run:(void (^)(void))blk {
    blk();
}
@end

static void gw_start_thread(void) {
    dispatch_once(&gBTOnce, ^{
        gLive = [NSMutableSet set];
        dispatch_semaphore_t ready = dispatch_semaphore_create(0);
        gBTThread = [[NSThread alloc] initWithBlock:^{
            @autoreleasepool {
                NSRunLoop *rl = [NSRunLoop currentRunLoop];
                // A port source keeps the run loop alive with no other sources.
                [rl addPort:[NSMachPort port] forMode:NSDefaultRunLoopMode];
                dispatch_semaphore_signal(ready);
                [rl run];
            }
        }];
        gBTThread.name = @"graywolf-iobluetooth";
        [gBTThread start];
        dispatch_semaphore_wait(ready, DISPATCH_TIME_FOREVER);
    });
}

// Runs blk on the IOBluetooth thread. wait=YES blocks the caller until it
// returns; the IOBluetooth thread itself must never block on I/O.
static void gw_on_bt(BOOL wait, void (^blk)(void)) {
    gw_start_thread();
    [GWBlockRunner performSelector:@selector(run:)
                          onThread:gBTThread
                        withObject:[blk copy]
                     waitUntilDone:wait];
}

static void gw_seterr(char *err, int errlen, NSString *msg) {
    if (err && errlen > 0) {
        strlcpy(err, [msg UTF8String], (size_t)errlen);
    }
}

static NSString *gw_hex(IOReturn r) {
    return [NSString stringWithFormat:@"0x%x", (unsigned)r];
}

@interface GWSession : NSObject <IOBluetoothRFCOMMChannelDelegate>
@property(strong) IOBluetoothDevice *dev;
@property(strong) IOBluetoothRFCOMMChannel *ch;
@property(assign) int wfd;
@property(assign) BOOL closed;
@property(strong) dispatch_semaphore_t openSem;
@property(strong) dispatch_semaphore_t sdpSem;
@property(strong) dispatch_semaphore_t writeSem;
@property(assign) IOReturn openStatus;
@property(assign) IOReturn sdpStatus;
@property(assign) IOReturn writeStatus;
@property(strong) NSMutableData *pending;
@property(strong) NSLock *wlock; // serialises pipe close vs. data callbacks
@property(strong) NSLock *writeMu;
- (void)endPipe;
@end

@implementation GWSession

- (void)endPipe {
    [self.wlock lock];
    if (self.wfd >= 0) {
        close(self.wfd);
        self.wfd = -1;
    }
    [self.wlock unlock];
}

- (void)rfcommChannelOpenComplete:(IOBluetoothRFCOMMChannel *)ch status:(IOReturn)error {
    self.openStatus = error;
    dispatch_semaphore_signal(self.openSem);
}

- (void)rfcommChannelData:(IOBluetoothRFCOMMChannel *)ch data:(void *)ptr length:(size_t)len {
    [self.wlock lock];
    const uint8_t *p = (const uint8_t *)ptr;
    size_t off = 0;
    while (self.wfd >= 0 && off < len) {
        ssize_t n = write(self.wfd, p + off, len - off);
        if (n < 0) {
            if (errno == EINTR) continue;
            break;
        }
        off += (size_t)n;
    }
    [self.wlock unlock];
}

- (void)rfcommChannelClosed:(IOBluetoothRFCOMMChannel *)ch {
    [self endPipe];
}

- (void)rfcommChannelWriteComplete:(IOBluetoothRFCOMMChannel *)ch refcon:(void *)refcon status:(IOReturn)error {
    self.writeStatus = error;
    if (self.writeSem) dispatch_semaphore_signal(self.writeSem);
}

- (void)sdpQueryComplete:(IOBluetoothDevice *)device status:(IOReturn)status {
    self.sdpStatus = status;
    if (self.sdpSem) dispatch_semaphore_signal(self.sdpSem);
}

@end

// IOBluetooth's own getRFCOMMChannelID returns the wrong channel for some
// radios (an Icom ID-5200 reports 1 for a Serial Port record that says 3),
// so the RFCOMM channel is read from the record's ProtocolDescriptorList.
static uint32_t gw_uuid_value(IOBluetoothSDPUUID *u) {
    uint32_t v = 0;
    const uint8_t *b = (const uint8_t *)[u bytes];
    NSUInteger n = [u length];
    if (!b || n == 0 || n > 4) return 0;
    for (NSUInteger i = 0; i < n; i++) v = (v << 8) | b[i];
    return v;
}

static int gw_record_rfcomm_channel(IOBluetoothSDPServiceRecord *rec) {
    IOBluetoothSDPDataElement *pdl = [rec getAttributeDataElement:kBluetoothSDPAttributeIdentifierProtocolDescriptorList];
    for (id p in [pdl getArrayValue]) {
        NSArray *items = [(IOBluetoothSDPDataElement *)p getArrayValue];
        if (items.count < 2) continue;
        IOBluetoothSDPUUID *u = [(IOBluetoothSDPDataElement *)items[0] getUUIDValue];
        if (u && gw_uuid_value(u) == kBluetoothSDPUUID16RFCOMM) {
            return [[(IOBluetoothSDPDataElement *)items[1] getNumberValue] intValue];
        }
    }
    return 0;
}

static uint8_t gw_spp_channel(IOBluetoothDevice *dev) {
    IOBluetoothSDPUUID *spp = [IOBluetoothSDPUUID uuid16:kBluetoothSDPUUID16ServiceClassSerialPort];
    for (id r in dev.services) {
        IOBluetoothSDPServiceRecord *rec = (IOBluetoothSDPServiceRecord *)r;
        if (![rec hasServiceFromArray:@[ spp ]]) continue;
        int c = gw_record_rfcomm_channel(rec);
        if (c > 0 && c < 31) return (uint8_t)c;
    }
    return 0;
}

static void gw_shutdown(GWSession *s) {
    if (s.closed) return;
    s.closed = YES;
    IOBluetoothDevice *dev = s.dev;
    IOBluetoothRFCOMMChannel *ch = s.ch;
    gw_on_bt(YES, ^{
        if (ch) [ch closeChannel];
        if (dev) [dev closeConnection];
    });
    [s endPipe];
    // IOBluetooth keeps an unretained pointer to its delegate; hold the
    // session a while after close so a late callback cannot hit freed memory.
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC),
                   dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
        @synchronized(gLive) { [gLive removeObject:s]; }
    });
}

uintptr_t gw_rfcomm_open(const char *addr, int channelHint, int *rfd, char *err, int errlen) {
    @autoreleasepool {
        NSString *a = [NSString stringWithUTF8String:addr];
        __block IOBluetoothDevice *dev = nil;
        gw_on_bt(YES, ^{ dev = [IOBluetoothDevice deviceWithAddressString:a]; });
        if (!dev) {
            gw_seterr(err, errlen, @"unknown Bluetooth address");
            return 0;
        }
        __block BOOL paired = NO, connected = NO;
        gw_on_bt(YES, ^{ paired = [dev isPaired]; connected = [dev isConnected]; });
        if (!paired) {
            gw_seterr(err, errlen, @"device is not paired with this Mac");
            return 0;
        }
        if (connected) {
            // The radio reconnected on its own (or a previous session left the
            // baseband up). The radio accepts only one serial connection, so
            // drop that link before dialing it ourselves.
            gw_on_bt(YES, ^{ [dev closeConnection]; });
            usleep(1500 * 1000);
        }

        GWSession *s = [[GWSession alloc] init];
        s.dev = dev;
        s.wfd = -1;
        s.wlock = [[NSLock alloc] init];
        s.writeMu = [[NSLock alloc] init];
        s.openSem = dispatch_semaphore_create(0);

        uint8_t chid = (uint8_t)channelHint;
        if (chid == 0) {
            __block uint8_t found = 0;
            gw_on_bt(YES, ^{ found = gw_spp_channel(dev); });
            if (found == 0) {
                s.sdpSem = dispatch_semaphore_create(0);
                __block IOReturn qr = kIOReturnError;
                gw_on_bt(YES, ^{ qr = [dev performSDPQuery:s]; });
                if (qr == kIOReturnSuccess &&
                    dispatch_semaphore_wait(s.sdpSem, dispatch_time(DISPATCH_TIME_NOW, 20 * NSEC_PER_SEC)) == 0) {
                    gw_on_bt(YES, ^{ found = gw_spp_channel(dev); });
                }
            }
            if (found == 0) {
                gw_seterr(err, errlen, @"device does not advertise a Serial Port (SPP) service");
                return 0;
            }
            chid = found;
        }

        int fds[2];
        if (pipe(fds) != 0) {
            gw_seterr(err, errlen, @"pipe failed");
            return 0;
        }
        // Close-on-exec: a leaked fd in graywolf-modem would hold the link open.
        fcntl(fds[0], F_SETFD, FD_CLOEXEC);
        fcntl(fds[1], F_SETFD, FD_CLOEXEC);
        fcntl(fds[1], F_SETNOSIGPIPE, 1);
        s.wfd = fds[1];

        @synchronized(gLive) { [gLive addObject:s]; }

        __block IOReturn r = kIOReturnError;
        gw_on_bt(YES, ^{
            IOBluetoothRFCOMMChannel *ch = nil;
            r = [dev openRFCOMMChannelAsync:&ch withChannelID:chid delegate:s];
            s.ch = ch;
        });
        if (r != kIOReturnSuccess) {
            gw_seterr(err, errlen, [NSString stringWithFormat:@"openRFCOMMChannel channel %d failed: %@", chid, gw_hex(r)]);
            close(fds[0]);
            gw_shutdown(s);
            return 0;
        }
        if (dispatch_semaphore_wait(s.openSem, dispatch_time(DISPATCH_TIME_NOW, 25 * NSEC_PER_SEC)) != 0) {
            gw_seterr(err, errlen, [NSString stringWithFormat:@"RFCOMM channel %d open timed out", chid]);
            close(fds[0]);
            gw_shutdown(s);
            return 0;
        }
        if (s.openStatus != kIOReturnSuccess) {
            gw_seterr(err, errlen, [NSString stringWithFormat:@"RFCOMM channel %d refused: %@ (is the radio already connected to this Mac?)", chid, gw_hex(s.openStatus)]);
            close(fds[0]);
            gw_shutdown(s);
            return 0;
        }
        *rfd = fds[0];
        return (uintptr_t)CFBridgingRetain(s);
    }
}

int gw_rfcomm_write(uintptr_t h, const void *buf, int n, int timeoutMs, char *err, int errlen) {
    @autoreleasepool {
        GWSession *s = (__bridge GWSession *)(void *)h;
        if (s.closed) {
            gw_seterr(err, errlen, @"channel closed");
            return -1;
        }
        [s.writeMu lock];
        int rc = 0;
        __block UInt16 mtu = 127;
        gw_on_bt(YES, ^{
            UInt16 m = [s.ch getMTU];
            if (m > 0) mtu = m;
        });
        int off = 0;
        while (off < n) {
            int chunk = n - off;
            if (chunk > mtu) chunk = mtu;
            s.pending = [NSMutableData dataWithBytes:(const uint8_t *)buf + off length:(NSUInteger)chunk];
            s.writeSem = dispatch_semaphore_create(0);
            s.writeStatus = kIOReturnSuccess;
            __block IOReturn r = kIOReturnError;
            gw_on_bt(YES, ^{
                r = [s.ch writeAsync:s.pending.mutableBytes length:(UInt16)chunk refcon:NULL];
            });
            if (r != kIOReturnSuccess) {
                gw_seterr(err, errlen, [NSString stringWithFormat:@"writeAsync failed: %@", gw_hex(r)]);
                rc = -1;
                break;
            }
            if (dispatch_semaphore_wait(s.writeSem, dispatch_time(DISPATCH_TIME_NOW, (int64_t)timeoutMs * NSEC_PER_MSEC)) != 0) {
                gw_seterr(err, errlen, @"write timed out");
                rc = -1;
                break;
            }
            if (s.writeStatus != kIOReturnSuccess) {
                gw_seterr(err, errlen, [NSString stringWithFormat:@"write failed: %@", gw_hex(s.writeStatus)]);
                rc = -1;
                break;
            }
            off += chunk;
        }
        [s.writeMu unlock];
        return rc;
    }
}

void gw_rfcomm_close(uintptr_t h) {
    @autoreleasepool {
        GWSession *s = (GWSession *)CFBridgingRelease((CFTypeRef)h);
        gw_shutdown(s);
    }
}

char *gw_bt_paired_json(void) {
    @autoreleasepool {
        __block NSArray *devs = nil;
        gw_on_bt(YES, ^{ devs = [IOBluetoothDevice pairedDevices]; });
        IOBluetoothSDPUUID *spp = [IOBluetoothSDPUUID uuid16:kBluetoothSDPUUID16ServiceClassSerialPort];
        NSMutableArray *withSPP = [NSMutableArray array];
        NSMutableArray *all = [NSMutableArray array];
        for (IOBluetoothDevice *d in devs) {
            NSString *mac = [[[d addressString] uppercaseString] stringByReplacingOccurrencesOfString:@"-" withString:@":"];
            if (!mac) continue;
            NSDictionary *e = @{@"mac" : mac, @"name" : [d name] ?: @""};
            [all addObject:e];
            for (id r in d.services) {
                if ([(IOBluetoothSDPServiceRecord *)r hasServiceFromArray:@[ spp ]]) {
                    [withSPP addObject:e];
                    break;
                }
            }
        }
        // Prefer devices known to offer a Serial Port service (headphones,
        // controllers and phones are noise); fall back to everything when
        // none are cached yet.
        NSData *j = [NSJSONSerialization dataWithJSONObject:(withSPP.count ? withSPP : all) options:0 error:nil];
        if (!j) return NULL;
        char *s = malloc(j.length + 1);
        memcpy(s, j.bytes, j.length);
        s[j.length] = 0;
        return s;
    }
}
