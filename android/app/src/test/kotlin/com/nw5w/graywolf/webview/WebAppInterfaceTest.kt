package com.nw5w.graywolf.webview

import org.junit.Assert.assertEquals
import org.junit.Test

class WebAppInterfaceTest {
    @Test
    fun getBearerToken_returnsProvidedValue() {
        val iface = WebAppInterface(tokenProvider = { "abc-123" })
        assertEquals("abc-123", iface.getBearerToken())
    }

    @Test
    fun pttMethodsAreNotExposed() {
        // Phase 3 removes the POC-D PTT trigger surface; phase 5
        // rewires it through the proto path. This test fails if
        // anyone re-adds keyCp2102nRts / keyCm108Hid /
        // keyAiocCdcRts / fireTxTest to the public surface.
        val methods = WebAppInterface(tokenProvider = { "x" })::class.java
            .declaredMethods
            .map { it.name }
            .toSet()
        val forbidden = setOf(
            "fireTxTest",
            "pttStatusJson",
            "keyCp2102nRts", "unkeyCp2102nRts",
            "keyCm108Hid", "unkeyCm108Hid",
            "setCm108Bit",
            "keyAiocCdcRts", "unkeyAiocCdcRts",
        )
        val present = methods.intersect(forbidden)
        assertEquals(emptySet<String>(), present)
    }
}
