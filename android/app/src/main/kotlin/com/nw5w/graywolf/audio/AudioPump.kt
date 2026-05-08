package com.nw5w.graywolf.audio

import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.util.Log
import com.nw5w.graywolf.jni.ModemBridge
import kotlin.concurrent.thread

class AudioPump {
    @Volatile private var running = false
    private var recorder: AudioRecord? = null
    private var thread: Thread? = null

    fun start(sampleRate: Int = 22050) {
        if (running) return
        val minBytes = AudioRecord.getMinBufferSize(
            sampleRate,
            AudioFormat.CHANNEL_IN_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
        )
        check(minBytes > 0) { "getMinBufferSize=$minBytes" }
        val bufBytes = minBytes * 4
        val rec = AudioRecord(
            MediaRecorder.AudioSource.MIC,
            sampleRate,
            AudioFormat.CHANNEL_IN_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
            bufBytes,
        )
        check(rec.state == AudioRecord.STATE_INITIALIZED) {
            "AudioRecord state=${rec.state}"
        }
        Log.i(TAG, "AudioRecord init rate=$sampleRate bufBytes=$bufBytes (min=$minBytes)")
        rec.startRecording()
        recorder = rec
        running = true
        val bufShorts = bufBytes / 2
        thread = thread(name = "audio-pump", priority = Thread.MAX_PRIORITY) {
            val scratch = ShortArray(bufShorts)
            while (running) {
                val n = rec.read(scratch, 0, scratch.size)
                if (n > 0) {
                    ModemBridge.modemPushSamples(scratch, n)
                } else if (n < 0) {
                    Log.w(TAG, "AudioRecord.read=$n")
                }
            }
            try { rec.stop() } catch (_: Throwable) {}
            rec.release()
            Log.i(TAG, "audio pump exited")
        }
    }

    fun stop() {
        running = false
        thread?.join(2000)
        thread = null
        recorder = null
    }

    companion object { private const val TAG = "AudioPump" }
}
