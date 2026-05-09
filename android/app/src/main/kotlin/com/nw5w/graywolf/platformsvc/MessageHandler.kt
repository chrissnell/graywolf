package com.nw5w.graywolf.platformsvc

import com.nw5w.graywolf.platformproto.Error as ProtoError
import com.nw5w.graywolf.platformproto.ErrorCode
import com.nw5w.graywolf.platformproto.GpsFix
import com.nw5w.graywolf.platformproto.Hello
import com.nw5w.graywolf.platformproto.PlatformMessage

/**
 * Per-message-type handlers. Phase 2 implements only Hello and GpsFix;
 * everything else returns an Error{NOT_IMPLEMENTED} response (or, for
 * notification-style messages, is silently dropped after logging).
 */
internal sealed class MessageHandler {
    abstract fun handle(req: PlatformMessage): PlatformMessage?
}

internal class HelloHandler(
    private val serverVersion: String,
    private val schemaVersion: Int,
) : MessageHandler() {
    override fun handle(req: PlatformMessage): PlatformMessage {
        val incoming = req.hello
        if (incoming.schemaVersion != schemaVersion) {
            return PlatformMessage.newBuilder()
                .setError(ProtoError.newBuilder()
                    .setCode(ErrorCode.ERROR_SCHEMA_MISMATCH)
                    .setMessage("server speaks $schemaVersion, client speaks ${incoming.schemaVersion}")
                    .build())
                .build()
        }
        return PlatformMessage.newBuilder()
            .setHello(Hello.newBuilder()
                .setSchemaVersion(schemaVersion)
                .setClientVersion(incoming.clientVersion)
                .setServerVersion(serverVersion)
                .build())
            .build()
    }
}

internal class GpsFixHandler(
    private val onFix: (GpsFix) -> Unit,
) : MessageHandler() {
    override fun handle(req: PlatformMessage): PlatformMessage? {
        onFix(req.gpsFix)
        return null  // notification — no reply
    }
}

internal class NotImplementedHandler(private val typeLabel: String) : MessageHandler() {
    override fun handle(req: PlatformMessage): PlatformMessage =
        PlatformMessage.newBuilder()
            .setError(ProtoError.newBuilder()
                .setCode(ErrorCode.ERROR_NOT_IMPLEMENTED)
                .setMessage("$typeLabel not yet wired (phase 2 ships Hello + GpsFix only)")
                .build())
            .build()
}
