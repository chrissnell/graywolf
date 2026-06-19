package com.nw5w.graywolf.gps

import android.content.Context
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import com.nw5w.graywolf.platformproto.GpsSource
import com.nw5w.graywolf.platformsvc.PlatformServer
import org.junit.Test
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.assertFalse
import org.mockito.Mockito.mock
import org.mockito.Mockito.`when`
import org.mockito.kotlin.any
import org.mockito.kotlin.doThrow
import org.mockito.kotlin.eq
import org.mockito.kotlin.never
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever

class GpsAdapterTest {
    // Android framework classes are stubbed under the host JVM; we mock
    // Location, Context, and PlatformServer so the test stays hermetic.

    @Test fun toGpsFix_populatesAllFields() {
        val loc = mock(Location::class.java)
        `when`(loc.latitude).thenReturn(39.7392)
        `when`(loc.longitude).thenReturn(-104.9903)
        `when`(loc.hasAltitude()).thenReturn(true)
        `when`(loc.altitude).thenReturn(1609.0)
        `when`(loc.hasSpeed()).thenReturn(true)
        `when`(loc.speed).thenReturn(0.5f)
        `when`(loc.hasBearing()).thenReturn(true)
        `when`(loc.bearing).thenReturn(142.0f)
        `when`(loc.time).thenReturn(1_700_000_000_000L)
        `when`(loc.hasAccuracy()).thenReturn(true)
        `when`(loc.accuracy).thenReturn(4.8f)

        val adapter = GpsAdapter(mock(Context::class.java), mock(PlatformServer::class.java))
        val fix = adapter.toGpsFix(loc, 8)

        assertEquals(39.7392, fix.lat, 1e-9)
        assertEquals(-104.9903, fix.lon, 1e-9)
        assertEquals(1609.0, fix.altM, 1e-9)
        assertEquals(0.5, fix.speedMps, 1e-6)
        assertEquals(142.0, fix.courseDeg, 1e-6)
        assertEquals(1_700_000_000_000L, fix.timeUnixMs)
        assertEquals(4.8, fix.accuracyM, 1e-6)
        assertEquals(8, fix.numSats.toInt())
        assertEquals(GpsSource.GPS_SOURCE_ANDROID_GPS, fix.source)
        assertTrue(fix.hdop == 0.0)
        assertTrue(fix.hasAlt)
        assertTrue(fix.hasSpeed)
        assertTrue(fix.hasCourse)
    }

    @Test fun toGpsFix_defaultsWhenLocationFieldsAbsent() {
        val loc = mock(Location::class.java)
        `when`(loc.latitude).thenReturn(0.0)
        `when`(loc.longitude).thenReturn(0.0)
        `when`(loc.hasAltitude()).thenReturn(false)
        `when`(loc.hasSpeed()).thenReturn(false)
        `when`(loc.hasBearing()).thenReturn(false)
        `when`(loc.hasAccuracy()).thenReturn(false)
        `when`(loc.time).thenReturn(0L)

        val adapter = GpsAdapter(mock(Context::class.java), mock(PlatformServer::class.java))
        val fix = adapter.toGpsFix(loc, 0)

        assertEquals(0.0, fix.altM, 1e-9)
        assertEquals(0.0, fix.speedMps, 1e-9)
        assertEquals(0.0, fix.courseDeg, 1e-9)
        assertEquals(0.0, fix.accuracyM, 1e-9)
        assertEquals(0, fix.numSats.toInt())
        assertFalse(fix.hasAlt)
        assertFalse(fix.hasSpeed)
        assertFalse(fix.hasCourse)
    }

    // unitTests.isReturnDefaultValues makes the host-JVM permission check
    // return GRANTED (0), so start() runs past the permission gate and we can
    // drive its provider handling with a mocked LocationManager.

    @Test fun start_isNoOp_whenDeviceHasNoGpsProvider() {
        // Regression for #338: Facebook Portal / Android TV have no GPS_PROVIDER,
        // so requestLocationUpdates would throw IllegalArgumentException and crash
        // GraywolfService.onCreate. start() must skip cleanly instead.
        val lm = mock(LocationManager::class.java)
        `when`(lm.allProviders).thenReturn(listOf(LocationManager.NETWORK_PROVIDER))
        val ctx = mock(Context::class.java)
        `when`(ctx.getSystemService(Context.LOCATION_SERVICE)).thenReturn(lm)

        val adapter = GpsAdapter(ctx, mock(PlatformServer::class.java))
        adapter.start() // must not throw

        verify(lm, never()).requestLocationUpdates(
            any<String>(), any<Long>(), any<Float>(), any<LocationListener>()
        )
    }

    @Test fun start_swallowsIllegalArgument_whenProviderVanishesAfterCheck() {
        // Belt-and-suspenders: even if the provider is listed, a device may still
        // reject the request. start() must catch IllegalArgumentException, not crash.
        val lm = mock(LocationManager::class.java)
        `when`(lm.allProviders).thenReturn(listOf(LocationManager.GPS_PROVIDER))
        doThrow(IllegalArgumentException("provider doesn't exist: gps"))
            .whenever(lm).requestLocationUpdates(
                eq(LocationManager.GPS_PROVIDER), any<Long>(), any<Float>(), any<LocationListener>()
            )
        val ctx = mock(Context::class.java)
        `when`(ctx.getSystemService(Context.LOCATION_SERVICE)).thenReturn(lm)

        val adapter = GpsAdapter(ctx, mock(PlatformServer::class.java))
        adapter.start() // must not throw
    }
}
