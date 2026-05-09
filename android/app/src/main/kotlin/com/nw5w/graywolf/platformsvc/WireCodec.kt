package com.nw5w.graywolf.platformsvc

import com.nw5w.graywolf.platformproto.PlatformMessage
import java.io.EOFException
import java.io.InputStream
import java.io.OutputStream

/**
 * Length-prefix codec mirroring pkg/modembridge/framing.go and
 * pkg/platformsvc/framing.go.
 *
 * Wire layout: [4 BE bytes length][serialized PlatformMessage bytes]
 * Frames are capped at 64 KiB (the same limit as the Rust modem IPC).
 */
object WireCodec {
    const val MAX_FRAME_SIZE = 64 * 1024

    fun writeFrame(out: OutputStream, msg: PlatformMessage) {
        val buf = msg.toByteArray()
        check(buf.size <= MAX_FRAME_SIZE) {
            "frame too large: ${buf.size} > $MAX_FRAME_SIZE"
        }
        val hdr = ByteArray(4)
        hdr[0] = ((buf.size shr 24) and 0xFF).toByte()
        hdr[1] = ((buf.size shr 16) and 0xFF).toByte()
        hdr[2] = ((buf.size shr 8) and 0xFF).toByte()
        hdr[3] = (buf.size and 0xFF).toByte()
        out.write(hdr)
        out.write(buf)
        out.flush()
    }

    fun readFrame(input: InputStream): PlatformMessage {
        val hdr = readFully(input, 4)
        val n = ((hdr[0].toInt() and 0xFF) shl 24) or
                ((hdr[1].toInt() and 0xFF) shl 16) or
                ((hdr[2].toInt() and 0xFF) shl 8) or
                (hdr[3].toInt() and 0xFF)
        check(n in 0..MAX_FRAME_SIZE) {
            "frame too large: $n > $MAX_FRAME_SIZE"
        }
        val payload = readFully(input, n)
        return PlatformMessage.parseFrom(payload)
    }

    private fun readFully(input: InputStream, n: Int): ByteArray {
        val out = ByteArray(n)
        var read = 0
        while (read < n) {
            val r = input.read(out, read, n - read)
            if (r < 0) throw EOFException("unexpected EOF after $read bytes")
            read += r
        }
        return out
    }
}
