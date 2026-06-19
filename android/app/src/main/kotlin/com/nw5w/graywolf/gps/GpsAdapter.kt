package com.nw5w.graywolf.gps

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.GnssStatus
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.util.Log
import androidx.core.content.ContextCompat
import com.nw5w.graywolf.platformproto.GnssStatusUpdate
import com.nw5w.graywolf.platformproto.GpsFix
import com.nw5w.graywolf.platformproto.GpsSource
import com.nw5w.graywolf.platformproto.SatInfo
import com.nw5w.graywolf.platformsvc.PlatformServer

/**
 * GPS producer: subscribes to the system LocationManager, translates
 * each Location into a GpsFix proto and each GnssStatus callback into
 * a GnssStatusUpdate proto, and pushes them through PlatformServer's
 * server-to-client broadcast.
 *
 * Lifecycle: start() in GraywolfService.onCreate after PlatformServer.start;
 * stop() in onDestroy before PlatformServer.stop. start() is a silent
 * no-op if ACCESS_FINE_LOCATION is not granted — the user must re-grant
 * via system settings, then re-launch the app.
 */
class GpsAdapter(
    private val ctx: Context,
    private val server: PlatformServer,
) {
    // lazy so unit tests can construct GpsAdapter with a mocked Context
    // without resolving the system service (toGpsFix doesn't touch it).
    private val locationManager: LocationManager by lazy {
        ctx.getSystemService(Context.LOCATION_SERVICE) as LocationManager
    }

    @Volatile private var lastSatCount: Int = 0
    @Volatile private var started: Boolean = false

    private val locationListener = LocationListener { loc -> onLocation(loc) }

    private val gnssStatusCallback = object : GnssStatus.Callback() {
        override fun onSatelliteStatusChanged(status: GnssStatus) {
            var used = 0
            val builder = GnssStatusUpdate.newBuilder()
            for (i in 0 until status.satelliteCount) {
                val isUsed = status.usedInFix(i)
                if (isUsed) used++
                builder.addSats(SatInfo.newBuilder()
                    .setSvid(status.getSvid(i))
                    .setConstellation(constellationName(status.getConstellationType(i)))
                    .setCn0Dbhz(status.getCn0DbHz(i).toDouble())
                    .setUsedInFix(isUsed)
                    .setElevationDeg(status.getElevationDegrees(i).toDouble())
                    .setAzimuthDeg(status.getAzimuthDegrees(i).toDouble())
                    .build())
            }
            lastSatCount = used
            server.broadcastGnssStatus(builder
                .setSatsInView(status.satelliteCount)
                .setSatsUsed(used)
                .build())
        }
    }

    fun start() {
        if (started) return
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_FINE_LOCATION)
            != PackageManager.PERMISSION_GRANTED) {
            Log.i(TAG, "GpsAdapter.start skipped — ACCESS_FINE_LOCATION not granted")
            return
        }
        // Devices without a GPS HAL (Facebook Portal, Android TV boxes, the
        // emulator) have no GPS_PROVIDER, and requestLocationUpdates throws
        // IllegalArgumentException("provider doesn't exist: gps"). Skip cleanly,
        // same posture as the missing-permission case above: the app runs
        // without GPS rather than crashing onCreate (issue #338).
        if (LocationManager.GPS_PROVIDER !in locationManager.allProviders) {
            Log.i(TAG, "GpsAdapter.start skipped — no GPS provider on this device")
            return
        }
        try {
            locationManager.requestLocationUpdates(
                LocationManager.GPS_PROVIDER,
                10_000L, 0f, locationListener
            )
            locationManager.registerGnssStatusCallback(gnssStatusCallback, /* handler = */ null)
            started = true
            Log.i(TAG, "GpsAdapter started: GPS_PROVIDER 10s/0m + GNSS status callback")
        } catch (se: SecurityException) {
            Log.w(TAG, "GpsAdapter start hit SecurityException: $se")
            rollbackPartialStart()
        } catch (iae: IllegalArgumentException) {
            // Belt-and-suspenders for the no-provider race or a device where
            // registerGnssStatusCallback rejects the request after the guard.
            Log.w(TAG, "GpsAdapter start hit IllegalArgumentException: $iae")
            rollbackPartialStart()
        }
    }

    // requestLocationUpdates may have registered the listener before a later
    // call in start() threw; started stays false, so stop() (which only cleans
    // up when started) would leak it. Tear down whatever did register.
    private fun rollbackPartialStart() {
        try { locationManager.removeUpdates(locationListener) } catch (_: Throwable) {}
        try { locationManager.unregisterGnssStatusCallback(gnssStatusCallback) } catch (_: Throwable) {}
    }

    fun stop() {
        if (!started) return
        started = false
        try { locationManager.removeUpdates(locationListener) } catch (_: Throwable) {}
        try { locationManager.unregisterGnssStatusCallback(gnssStatusCallback) } catch (_: Throwable) {}
    }

    /** Visible for testing. */
    internal fun toGpsFix(loc: Location, satCount: Int): GpsFix {
        return GpsFix.newBuilder()
            .setLat(loc.latitude)
            .setLon(loc.longitude)
            .setAltM(if (loc.hasAltitude()) loc.altitude else 0.0)
            .setHasAlt(loc.hasAltitude())
            .setSpeedMps(if (loc.hasSpeed()) loc.speed.toDouble() else 0.0)
            .setHasSpeed(loc.hasSpeed())
            .setCourseDeg(if (loc.hasBearing()) loc.bearing.toDouble() else 0.0)
            .setHasCourse(loc.hasBearing())
            .setTimeUnixMs(loc.time)
            .setHdop(0.0) // Android doesn't expose HDOP — see proto comment on accuracy_m.
            .setNumSats(satCount.coerceAtLeast(0))
            .setSource(GpsSource.GPS_SOURCE_ANDROID_GPS)
            .setAccuracyM(if (loc.hasAccuracy()) loc.accuracy.toDouble() else 0.0)
            .build()
    }

    private fun onLocation(loc: Location) {
        server.broadcastGpsFix(toGpsFix(loc, lastSatCount))
    }

    private fun constellationName(type: Int): String = when (type) {
        GnssStatus.CONSTELLATION_GPS -> "GPS"
        GnssStatus.CONSTELLATION_GLONASS -> "GLONASS"
        GnssStatus.CONSTELLATION_BEIDOU -> "BEIDOU"
        GnssStatus.CONSTELLATION_GALILEO -> "GALILEO"
        GnssStatus.CONSTELLATION_QZSS -> "QZSS"
        GnssStatus.CONSTELLATION_SBAS -> "SBAS"
        else -> "UNKNOWN"
    }

    companion object { private const val TAG = "GpsAdapter" }
}
