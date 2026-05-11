//go:build android

package platformsvc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// fakeServer pumps a single PlatformMessage back per client request, using
// net.Pipe instead of a real UDS so tests are hermetic.
type fakeServer struct {
	t       *testing.T
	conn    net.Conn
	respond func(*pb.PlatformMessage) *pb.PlatformMessage
	closeCh chan struct{}
	wg      sync.WaitGroup
}

func newFakeServer(t *testing.T, respond func(*pb.PlatformMessage) *pb.PlatformMessage) (*fakeServer, net.Conn) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	fs := &fakeServer{t: t, conn: serverConn, respond: respond, closeCh: make(chan struct{})}
	fs.wg.Add(1)
	go fs.serve()
	return fs, clientConn
}

func (fs *fakeServer) serve() {
	defer fs.wg.Done()
	for {
		select {
		case <-fs.closeCh:
			return
		default:
		}
		msg, err := readFrame(fs.conn)
		if err != nil {
			return
		}
		resp := fs.respond(msg)
		if resp == nil {
			continue
		}
		if err := writeFrame(fs.conn, resp); err != nil {
			return
		}
	}
}

func (fs *fakeServer) close() {
	close(fs.closeCh)
	fs.conn.Close()
	fs.wg.Wait()
}

// helloResponder returns Hello with matching schema version.
func helloResponder(serverVer uint32) func(*pb.PlatformMessage) *pb.PlatformMessage {
	return func(req *pb.PlatformMessage) *pb.PlatformMessage {
		switch b := req.GetBody().(type) {
		case *pb.PlatformMessage_Hello:
			return &pb.PlatformMessage{Body: &pb.PlatformMessage_Hello{
				Hello: &pb.Hello{
					SchemaVersion: serverVer,
					ClientVersion: b.Hello.ClientVersion,
					ServerVersion: "v0.0.0-test-server",
				},
			}}
		default:
			return &pb.PlatformMessage{Body: &pb.PlatformMessage_Error{
				Error: &pb.Error{Code: pb.ErrorCode_ERROR_NOT_IMPLEMENTED},
			}}
		}
	}
}

// withFakeClient wires up a client around a fake-server pipe so tests can
// drive Hello / subscription / PTT-request paths without binding a real UDS.
func withFakeClient(t *testing.T, respond func(*pb.PlatformMessage) *pb.PlatformMessage, fn func(*clientImpl)) {
	t.Helper()
	fs, clientConn := newFakeServer(t, respond)
	defer fs.close()
	c := newClient("").(*clientImpl)
	c.injectConn(clientConn)
	fn(c)
}

func TestHelloMatchingVersion(t *testing.T) {
	withFakeClient(t, helloResponder(SchemaVersion), func(c *clientImpl) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		resp, err := c.Hello(ctx, SchemaVersion)
		if err != nil {
			t.Fatalf("Hello: %v", err)
		}
		if resp.SchemaVersion != SchemaVersion {
			t.Errorf("schema_version: got %d, want %d", resp.SchemaVersion, SchemaVersion)
		}
	})
}

func TestSubscribeGpsFix(t *testing.T) {
	respond := func(req *pb.PlatformMessage) *pb.PlatformMessage {
		return nil // server doesn't reply to subscription requests; pushes asynchronously
	}
	fs, clientConn := newFakeServer(t, respond)
	defer fs.close()
	c := newClient("").(*clientImpl)
	c.injectConn(clientConn)

	ch := make(chan *pb.GpsFix, 1)
	if err := c.SubscribeGpsFix(context.Background(), ch); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	push := &pb.PlatformMessage{Body: &pb.PlatformMessage_GpsFix{
		GpsFix: &pb.GpsFix{Lat: 37.7749, Lon: -122.4194},
	}}
	if err := writeFrame(fs.conn, push); err != nil {
		t.Fatalf("server push: %v", err)
	}
	select {
	case fix := <-ch:
		if fix.Lat != 37.7749 {
			t.Errorf("lat: got %v", fix.Lat)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fix")
	}
}

func TestSubscribeGnssStatusDelivers(t *testing.T) {
	respond := func(req *pb.PlatformMessage) *pb.PlatformMessage { return nil }
	fs, clientConn := newFakeServer(t, respond)
	defer fs.close()
	c := newClient("").(*clientImpl)
	c.injectConn(clientConn)

	ch := make(chan *pb.GnssStatusUpdate, 4)
	if err := c.SubscribeGnssStatus(context.Background(), ch); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	push := &pb.PlatformMessage{Body: &pb.PlatformMessage_GnssStatus{
		GnssStatus: &pb.GnssStatusUpdate{
			SatsInView: 11,
			SatsUsed:   8,
			Sats: []*pb.SatInfo{
				{Svid: 5, Constellation: "GPS", Cn0Dbhz: 41.5, UsedInFix: true},
			},
		},
	}}
	if err := writeFrame(fs.conn, push); err != nil {
		t.Fatalf("server push: %v", err)
	}

	select {
	case got := <-ch:
		if got.GetSatsInView() != 11 || got.GetSatsUsed() != 8 || len(got.GetSats()) != 1 {
			t.Fatalf("unexpected payload: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for GnssStatusUpdate")
	}
}

func TestGnssStatusDoesNotCrossTalkToGpsFixSub(t *testing.T) {
	respond := func(req *pb.PlatformMessage) *pb.PlatformMessage { return nil }
	fs, clientConn := newFakeServer(t, respond)
	defer fs.close()
	c := newClient("").(*clientImpl)
	c.injectConn(clientConn)

	gpsCh := make(chan *pb.GpsFix, 4)
	gnssCh := make(chan *pb.GnssStatusUpdate, 4)
	_ = c.SubscribeGpsFix(context.Background(), gpsCh)
	_ = c.SubscribeGnssStatus(context.Background(), gnssCh)

	push := &pb.PlatformMessage{Body: &pb.PlatformMessage_GnssStatus{
		GnssStatus: &pb.GnssStatusUpdate{SatsInView: 1},
	}}
	_ = writeFrame(fs.conn, push)

	select {
	case <-gnssCh:
		// good
	case <-time.After(time.Second):
		t.Fatal("expected GnssStatus on its channel")
	}

	select {
	case got := <-gpsCh:
		t.Fatalf("GpsFix subscriber leaked a GnssStatus event: %+v", got)
	case <-time.After(200 * time.Millisecond):
		// good
	}
}

func TestRoundTripAllMessageTypes(t *testing.T) {
	// Echo server: every request gets a typed response of the same oneof.
	respond := func(req *pb.PlatformMessage) *pb.PlatformMessage {
		switch req.GetBody().(type) {
		case *pb.PlatformMessage_UsbListReq:
			return &pb.PlatformMessage{Body: &pb.PlatformMessage_UsbListResp{
				UsbListResp: &pb.UsbDeviceListResponse{},
			}}
		case *pb.PlatformMessage_UsbSelectReq:
			return &pb.PlatformMessage{Body: &pb.PlatformMessage_UsbSelectResp{
				UsbSelectResp: &pb.UsbSelectResponse{Granted: true, HandleId: "h-1"},
			}}
		case *pb.PlatformMessage_PttKeyReq, *pb.PlatformMessage_PttUnkeyReq:
			return &pb.PlatformMessage{Body: &pb.PlatformMessage_PttAck{
				PttAck: &pb.PttAck{Ok: true},
			}}
		}
		return nil
	}
	withFakeClient(t, respond, func(c *clientImpl) {
		ctx := context.Background()
		if _, err := c.ListUsbDevices(ctx, pb.UsbClass_USB_CLASS_HID); err != nil {
			t.Errorf("ListUsbDevices: %v", err)
		}
		h, err := c.SelectUsbDevice(ctx, 0x10C4, 0xEA60)
		if err != nil {
			t.Fatalf("SelectUsbDevice: %v", err)
		}
		if _, err := c.KeyPtt(ctx, pb.PttMethod_PTT_METHOD_CP2102N_RTS, h); err != nil {
			t.Errorf("KeyPtt: %v", err)
		}
		if _, err := c.UnkeyPtt(ctx, pb.PttMethod_PTT_METHOD_CP2102N_RTS, h); err != nil {
			t.Errorf("UnkeyPtt: %v", err)
		}
	})
}
