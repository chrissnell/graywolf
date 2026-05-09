package com.nw5w.graywolf.platformsvc

import com.nw5w.graywolf.platformproto.Hello
import com.nw5w.graywolf.platformproto.PlatformMessage
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.EOFException

class WireCodecTest {
    @Test
    fun roundTripHello() {
        val msg = PlatformMessage.newBuilder()
            .setHello(Hello.newBuilder().setSchemaVersion(1).setClientVersion("v0-test").build())
            .build()

        val out = ByteArrayOutputStream()
        WireCodec.writeFrame(out, msg)

        val got = WireCodec.readFrame(ByteArrayInputStream(out.toByteArray()))
        assertEquals(1, got.hello.schemaVersion)
        assertEquals("v0-test", got.hello.clientVersion)
    }

    @Test
    fun emptyPayloadOk() {
        val msg = PlatformMessage.newBuilder().build()
        val out = ByteArrayOutputStream()
        WireCodec.writeFrame(out, msg)
        val got = WireCodec.readFrame(ByteArrayInputStream(out.toByteArray()))
        assertEquals(PlatformMessage.BodyCase.BODY_NOT_SET, got.bodyCase)
    }

    @Test
    fun truncatedHeaderThrows() {
        val truncated = byteArrayOf(0, 1)
        assertThrows(EOFException::class.java) {
            WireCodec.readFrame(ByteArrayInputStream(truncated))
        }
    }

    @Test
    fun truncatedPayloadThrows() {
        val truncated = byteArrayOf(0, 0, 0, 0x10, 1, 2, 3)
        assertThrows(EOFException::class.java) {
            WireCodec.readFrame(ByteArrayInputStream(truncated))
        }
    }

    @Test
    fun oversizedFrameThrows() {
        val oversized = byteArrayOf(0xFF.toByte(), 0xFF.toByte(), 0xFF.toByte(), 0xFF.toByte())
        assertThrows(IllegalStateException::class.java) {
            WireCodec.readFrame(ByteArrayInputStream(oversized))
        }
    }
}
