package com.nw5w.graywolf

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import com.nw5w.graywolf.jni.ModemBridge

class GraywolfService : Service() {
    override fun onCreate() {
        super.onCreate()
        val mgr = getSystemService(NotificationManager::class.java)!!
        mgr.createNotificationChannel(
            NotificationChannel(
                getString(R.string.notification_channel_id),
                "graywolf foreground",
                NotificationManager.IMPORTANCE_LOW,
            )
        )
        val notif: Notification = Notification.Builder(this, getString(R.string.notification_channel_id))
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(getString(R.string.notification_text))
            .setSmallIcon(android.R.drawable.ic_media_play)
            .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(
                NOTIF_ID, notif,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
            )
        } else {
            startForeground(NOTIF_ID, notif)
        }
        val v = try {
            ModemBridge.modemVersion()
        } catch (t: Throwable) {
            Log.e(TAG, "modemVersion threw: $t")
            "ERROR"
        }
        Log.i(TAG, "modem cdylib version=$v")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int = START_STICKY

    override fun onBind(intent: Intent?): IBinder? = null

    companion object {
        private const val TAG = "GraywolfService"
        private const val NOTIF_ID = 0x6757
    }
}
