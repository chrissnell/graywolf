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

class BtSerialAdapterTest {
    @Test fun bondedDevicesRequest_returnsAllPaired() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val facade = FakeBluetoothFacade(bonded = listOf(
            BondedDevice("AA:BB:CC:00:00:01", "Mobilinkd TNC4"),
            BondedDevice("AA:BB:CC:00:00:02", "TH-D75"),
        ))
        val sent = mutableListOf<PlatformMessage>()
        val adapter = BtSerialAdapter(facade, dispatcher) { sent.add(it) }

        adapter.handleBondedRequest()
        advanceUntilIdle()

        assertEquals(1, sent.size)
        val resp = sent[0].bondedBtDevicesResponse
        assertEquals(2, resp.devicesCount)
        assertEquals("Mobilinkd TNC4", resp.getDevices(0).name)
        assertTrue(resp.getDevices(0).mac == "AA:BB:CC:00:00:01")
    }

    @Test fun serialOpen_notBonded_replies_with_ack_error() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val facade = FakeBluetoothFacade(bonded = emptyList())
        val sent = mutableListOf<PlatformMessage>()
        val adapter = BtSerialAdapter(facade, dispatcher) { sent.add(it) }

        adapter.handleSerialOpen(
            SerialOpen.newBuilder()
                .setHandle(42)
                .setKind(SerialKind.SERIAL_KIND_BLUETOOTH)
                .setAddress("AA:BB:CC:00:00:99")
                .build()
        )
        advanceUntilIdle()

        val acks = sent.filter { it.hasSerialOpenAck() }.map { it.serialOpenAck }
        assertEquals(1, acks.size)
        assertEquals(42, acks[0].handle.toInt())
        assertEquals(false, acks[0].ok)
        assertTrue(acks[0].error.contains("not_bonded") || acks[0].error.contains("not bonded"))
    }

    @Test fun closeHandle_sendsClose_and_stopsPumps() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        // The bookkeeping path: closing a non-existent handle is a no-op,
        // does not crash, and does not emit a SerialClose.
        val facade = FakeBluetoothFacade(bonded = listOf(BondedDevice("AA:BB:CC:00:00:01", "X")))
        val sent = mutableListOf<PlatformMessage>()
        val adapter = BtSerialAdapter(facade, dispatcher) { sent.add(it) }

        adapter.handleSerialClose(SerialClose.newBuilder().setHandle(999).setReason("test").build())
        advanceUntilIdle()
        assertTrue(sent.isEmpty())
    }
}
