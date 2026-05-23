@file:OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)

package com.nw5w.graywolf.platformsvc

import com.nw5w.graywolf.platformproto.PlatformMessage
import com.nw5w.graywolf.platformproto.SerialClose
import com.nw5w.graywolf.platformproto.SerialKind
import com.nw5w.graywolf.platformproto.SerialOpen
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class UsbSerialAdapterTest {

    private fun fakeFacade(
        devices: List<UsbSerialDeviceInfo> = emptyList(),
        open: (String, Int) -> UsbSerialSession = { _, _ ->
            throw UsbSerialOpenException("not_found")
        },
    ) = FakeUsbSerialFacade(devices, open)

    @Test fun availableRequest_returnsAllSerialDevices() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val facade = fakeFacade(devices = listOf(
            UsbSerialDeviceInfo("2341:0043", "TH-D75", "Kenwood", true),
            UsbSerialDeviceInfo("10c4:ea60", "Digirig", "Silicon Labs", false),
        ))
        val sent = mutableListOf<PlatformMessage>()
        val adapter = UsbSerialAdapter(facade, dispatcher) { sent.add(it) }

        adapter.handleAvailableRequest()
        advanceUntilIdle()

        assertEquals(1, sent.size)
        val resp = sent[0].availableUsbSerialDevicesResponse
        assertEquals(2, resp.devicesCount)
        assertEquals("2341:0043", resp.getDevices(0).vidPid)
        assertTrue(resp.getDevices(0).hasPermission)
        assertEquals("10c4:ea60", resp.getDevices(1).vidPid)
    }

    @Test fun serialOpen_notFound_repliesAckError() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val facade = fakeFacade(open = { _, _ -> throw UsbSerialOpenException("not_found") })
        val sent = mutableListOf<PlatformMessage>()
        val adapter = UsbSerialAdapter(facade, dispatcher) { sent.add(it) }

        adapter.handleSerialOpen(
            SerialOpen.newBuilder()
                .setHandle(7)
                .setKind(SerialKind.SERIAL_KIND_USB)
                .setAddress("2341:0043")
                .setBaud(9600)
                .build()
        )
        advanceUntilIdle()

        val acks = sent.filter { it.hasSerialOpenAck() }.map { it.serialOpenAck }
        assertEquals(1, acks.size)
        assertEquals(7, acks[0].handle.toInt())
        assertEquals(false, acks[0].ok)
        assertEquals("not_found", acks[0].error)
    }

    @Test fun serialOpen_permissionDenied_repliesAckError() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val facade = fakeFacade(open = { _, _ -> throw UsbSerialOpenException("permission_denied") })
        val sent = mutableListOf<PlatformMessage>()
        val adapter = UsbSerialAdapter(facade, dispatcher) { sent.add(it) }

        adapter.handleSerialOpen(
            SerialOpen.newBuilder()
                .setHandle(8).setKind(SerialKind.SERIAL_KIND_USB)
                .setAddress("2341:0043").setBaud(9600).build()
        )
        advanceUntilIdle()

        val ack = sent.first { it.hasSerialOpenAck() }.serialOpenAck
        assertEquals(false, ack.ok)
        assertEquals("permission_denied", ack.error)
    }

    @Test fun serialOpen_unsupported_repliesAckError() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val facade = fakeFacade(open = { _, _ -> throw UsbSerialOpenException("unsupported") })
        val sent = mutableListOf<PlatformMessage>()
        val adapter = UsbSerialAdapter(facade, dispatcher) { sent.add(it) }

        adapter.handleSerialOpen(
            SerialOpen.newBuilder()
                .setHandle(9).setKind(SerialKind.SERIAL_KIND_USB)
                .setAddress("dead:beef").setBaud(9600).build()
        )
        advanceUntilIdle()

        val ack = sent.first { it.hasSerialOpenAck() }.serialOpenAck
        assertEquals(false, ack.ok)
        assertEquals("unsupported", ack.error)
    }

    @Test fun openThenDetach_emitsErrorAndClose() {
        // Use real threads (Dispatchers.IO) for this test so the read pump's
        // blocking latch.await() does not deadlock the test scheduler thread.
        val sent = java.util.Collections.synchronizedList(mutableListOf<PlatformMessage>())
        val session = object : UsbSerialSession {
            override val deviceName = "/dev/bus/usb/001/004"
            @Volatile var closed = false
            private val latch = java.util.concurrent.CountDownLatch(1)
            override fun read(buf: ByteArray, timeoutMs: Int): Int {
                latch.await()
                return -1  // EOF after close() releases the latch
            }
            override fun write(bytes: ByteArray, timeoutMs: Int) {}
            override fun close() {
                closed = true
                latch.countDown()
            }
        }
        val facade = fakeFacade(open = { _, _ -> session })
        val adapter = UsbSerialAdapter(facade, kotlinx.coroutines.Dispatchers.IO) { sent.add(it) }

        adapter.handleSerialOpen(
            SerialOpen.newBuilder()
                .setHandle(11).setKind(SerialKind.SERIAL_KIND_USB)
                .setAddress("2341:0043").setBaud(9600).build()
        )
        // Wait for the open ack (real coroutine on IO threads)
        val deadline = System.currentTimeMillis() + 5_000
        while (System.currentTimeMillis() < deadline &&
               !sent.any { it.hasSerialOpenAck() && it.serialOpenAck.ok }) {
            Thread.sleep(10)
        }
        assertTrue("expected successful open ack", sent.any { it.hasSerialOpenAck() && it.serialOpenAck.ok })

        adapter.onUsbDetached("/dev/bus/usb/001/004")
        // Wait for error + close frames
        while (System.currentTimeMillis() < deadline &&
               !sent.any { it.hasSerialClose() && it.serialClose.handle.toInt() == 11 }) {
            Thread.sleep(10)
        }

        val errs = sent.filter { it.hasSerialError() }.map { it.serialError }
        assertTrue(errs.any { it.handle.toInt() == 11 && it.code == "usb_detached" })
        assertTrue(sent.any { it.hasSerialClose() && it.serialClose.handle.toInt() == 11 })
        assertTrue(session.closed)
        adapter.shutdown()
    }

    @Test fun closeUnknownHandle_isNoOp() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val sent = mutableListOf<PlatformMessage>()
        val adapter = UsbSerialAdapter(fakeFacade(), dispatcher) { sent.add(it) }

        adapter.handleSerialClose(SerialClose.newBuilder().setHandle(999).setReason("x").build())
        advanceUntilIdle()
        assertTrue(sent.isEmpty())
    }
}
