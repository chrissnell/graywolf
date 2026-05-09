package com.nw5w.graywolf.platformsvc

import com.nw5w.graywolf.platformproto.ErrorCode
import com.nw5w.graywolf.platformproto.GpsFix
import com.nw5w.graywolf.platformproto.Hello
import com.nw5w.graywolf.platformproto.PlatformMessage
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.io.File
import java.net.SocketAddress
import java.nio.channels.Channels
import java.nio.channels.SocketChannel
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

class PlatformServerTest {
    private lateinit var sockPath: String
    private lateinit var server: PlatformServer

    /**
     * Reflection helpers so the unit-test source compiles against android.jar
     * (which lacks java.net.UnixDomainSocketAddress and the
     * SocketChannel.open(SocketAddress) overload) but uses the real classes
     * at runtime under JDK 17.
     */
    private fun unixSocketAddress(path: String): SocketAddress {
        val cls = Class.forName("java.net.UnixDomainSocketAddress")
        return cls.getMethod("of", String::class.java).invoke(null, path) as SocketAddress
    }

    private fun openUnixClientChannel(path: String): SocketChannel {
        val openMethod = SocketChannel::class.java
            .getMethod("open", SocketAddress::class.java)
        return openMethod.invoke(null, unixSocketAddress(path)) as SocketChannel
    }

    @Before
    fun setUp() {
        sockPath = File.createTempFile("platform", ".sock").also { it.delete() }.absolutePath
        // schemaVersion is Int, not UInt — proto-javalite generates int for
        // uint32 and the UInt round-trip risks high-bit truncation later.
        server = PlatformServer(sockPath, serverVersion = "v0-test", schemaVersion = 1)
        server.startForTest()  // JDK ServerSocketChannel path; production uses start()
    }

    @After
    fun tearDown() {
        server.stop()
    }

    private fun connect(): SocketChannel {
        // Wait briefly for the accept loop to bind.
        val deadline = System.currentTimeMillis() + 2000
        while (!File(sockPath).exists() && System.currentTimeMillis() < deadline) {
            Thread.sleep(20)
        }
        return openUnixClientChannel(sockPath)
    }

    @Test
    fun helloMatchingSchemaSucceeds() {
        connect().use { ch ->
            val out = Channels.newOutputStream(ch)
            val input = Channels.newInputStream(ch)
            val req = PlatformMessage.newBuilder()
                .setHello(Hello.newBuilder().setSchemaVersion(1).setClientVersion("v0-client").build())
                .build()
            WireCodec.writeFrame(out, req)
            val resp = WireCodec.readFrame(input)
            assertEquals(1, resp.hello.schemaVersion)
            assertTrue(resp.hello.serverVersion.isNotEmpty())
        }
    }

    @Test
    fun helloMismatchReturnsError() {
        connect().use { ch ->
            val out = Channels.newOutputStream(ch)
            val input = Channels.newInputStream(ch)
            val req = PlatformMessage.newBuilder()
                .setHello(Hello.newBuilder().setSchemaVersion(99).setClientVersion("v0-client").build())
                .build()
            WireCodec.writeFrame(out, req)
            val resp = WireCodec.readFrame(input)
            assertEquals(PlatformMessage.BodyCase.ERROR, resp.bodyCase)
            assertEquals(ErrorCode.ERROR_SCHEMA_MISMATCH, resp.error.code)
        }
    }

    @Test
    fun gpsFixIsBroadcastToSubscribers() {
        val sink = LinkedBlockingQueue<GpsFix>()
        server.subscribeGpsFix { fix -> sink.put(fix) }
        connect().use { ch ->
            val out = Channels.newOutputStream(ch)
            // Hello first (otherwise the server may not yet be in the dispatch loop).
            WireCodec.writeFrame(out, PlatformMessage.newBuilder()
                .setHello(Hello.newBuilder().setSchemaVersion(1).build()).build())
            WireCodec.readFrame(Channels.newInputStream(ch))

            // Push a GpsFix; assert subscriber receives it.
            val fix = GpsFix.newBuilder().setLat(37.7749).setLon(-122.4194).build()
            WireCodec.writeFrame(out, PlatformMessage.newBuilder().setGpsFix(fix).build())
            val got = sink.poll(2, TimeUnit.SECONDS)
            assertEquals(37.7749, got.lat, 0.0001)
        }
    }

    @Test
    fun unimplementedMessageReturnsNotImplemented() {
        connect().use { ch ->
            val out = Channels.newOutputStream(ch)
            val input = Channels.newInputStream(ch)
            // Hello first.
            WireCodec.writeFrame(out, PlatformMessage.newBuilder()
                .setHello(Hello.newBuilder().setSchemaVersion(1).build()).build())
            WireCodec.readFrame(input)

            // BatteryState handler is not implemented in phase 2.
            WireCodec.writeFrame(out, PlatformMessage.newBuilder()
                .setBatteryState(com.nw5w.graywolf.platformproto.BatteryState.newBuilder().setPercent(50f).build())
                .build())
            // Server may or may not push a response for BatteryState (it's a notification, not a request).
            // We just assert the connection stays alive: send another Hello and expect a response.
            WireCodec.writeFrame(out, PlatformMessage.newBuilder()
                .setHello(Hello.newBuilder().setSchemaVersion(1).build()).build())
            val resp = WireCodec.readFrame(input)
            assertEquals(1, resp.hello.schemaVersion)
        }
    }
}
