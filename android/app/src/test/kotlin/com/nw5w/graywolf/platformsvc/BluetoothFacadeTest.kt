package com.nw5w.graywolf.platformsvc

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class BluetoothFacadeTest {
    @Test fun fakeFacade_returnsConfiguredBondedDevices() = runTest {
        val fake = FakeBluetoothFacade(bonded = listOf(
            BondedDevice("AA:BB:CC:00:00:01", "Mobilinkd TNC4"),
            BondedDevice("AA:BB:CC:00:00:02", "TH-D75"),
        ))
        val devices = fake.bondedDevices()
        assertEquals(2, devices.size)
        assertEquals("Mobilinkd TNC4", devices[0].name)
    }
}
