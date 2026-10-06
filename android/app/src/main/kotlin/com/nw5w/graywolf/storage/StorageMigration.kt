package com.nw5w.graywolf.storage

import android.content.Context
import android.os.Environment
import android.util.Log
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream

sealed class MigrationState {
    object Idle : MigrationState()
    object Starting : MigrationState()
    data class MigratingFile(val name: String, val bytesDone: Long, val totalBytes: Long) : MigrationState()
    object Complete : MigrationState()
    data class Error(val message: String) : MigrationState()

    fun toJson(): String = when (this) {
        is Idle -> """{"state":"idle","progress":0,"message":""}"""
        is Starting -> """{"state":"starting","progress":0,"message":"Preparing..."}"""
        is MigratingFile -> {
            val progress = if (totalBytes > 0) bytesDone.toDouble() / totalBytes else 0.0
            """{"state":"migrating","progress":${"%.4f".format(progress)},"bytes_done":$bytesDone,"total_bytes":$totalBytes,"message":${jsonString(name)}}"""
        }
        is Complete -> """{"state":"complete","progress":1,"message":""}"""
        is Error -> """{"state":"error","progress":0,"message":${jsonString(message)}}"""
    }

    private fun jsonString(s: String): String {
        val escaped = s.replace("\\", "\\\\").replace("\"", "\\\"")
        return "\"$escaped\""
    }
}

object StorageMigration {
    private const val TAG = "StorageMigration"
    private const val BUFFER_SIZE = 65_536
    // DB file names plus SQLite WAL/SHM siblings — only copy what exists.
    private val DB_NAMES = listOf(
        "graywolf.db", "graywolf.db-wal", "graywolf.db-shm",
        "graywolf-history.db", "graywolf-history.db-wal", "graywolf-history.db-shm",
        "graywolf-logs.db", "graywolf-logs.db-wal", "graywolf-logs.db-shm",
    )

    /**
     * Migrates data files from [srcDir] to [dstDir] with progress callbacks.
     * Returns true on success. The caller is responsible for stopping the Go
     * child before calling this and restarting it afterward.
     */
    fun migrate(
        ctx: Context,
        srcDir: File,
        dstDir: File,
        onState: (MigrationState) -> Unit,
    ): Boolean {
        onState(MigrationState.Starting)

        val files = collectFiles(srcDir)
        if (files.isEmpty()) {
            Log.w(TAG, "no data files found in $srcDir; nothing to migrate")
            onState(MigrationState.Complete)
            return true
        }

        val totalBytes = files.sumOf { it.length() }

        // Verify the destination has enough free space (1.1x headroom).
        val freeBytes = dstDir.freeSpace
        val required = (totalBytes * 1.1).toLong()
        if (freeBytes < required) {
            val msg = "Not enough space: need ${required / 1_048_576} MB, have ${freeBytes / 1_048_576} MB"
            Log.e(TAG, msg)
            onState(MigrationState.Error(msg))
            return false
        }

        val prefs = StoragePreferences(ctx)
        prefs.migrationInProgress = true

        var bytesDone = 0L
        try {
            for (file in files) {
                val relative = file.relativeTo(srcDir)
                val dst = File(dstDir, relative.path)
                dst.parentFile?.mkdirs()

                onState(MigrationState.MigratingFile(relative.path, bytesDone, totalBytes))
                Log.d(TAG, "copying ${relative.path} (${file.length()} bytes)")

                FileInputStream(file).use { input ->
                    FileOutputStream(dst).use { output ->
                        val buf = ByteArray(BUFFER_SIZE)
                        var n: Int
                        while (input.read(buf).also { n = it } != -1) {
                            output.write(buf, 0, n)
                            bytesDone += n
                            onState(MigrationState.MigratingFile(relative.path, bytesDone, totalBytes))
                        }
                        output.flush()
                    }
                }

                // Spot-check: destination size must match source.
                if (dst.length() != file.length()) {
                    val msg = "Size mismatch after copy: ${relative.path}"
                    Log.e(TAG, msg)
                    prefs.migrationInProgress = false
                    onState(MigrationState.Error(msg))
                    return false
                }
            }

            // Delete source files only after all copies are verified.
            for (file in files) {
                if (!file.delete()) Log.w(TAG, "could not delete source: ${file.path}")
            }
            // Remove tiles source directory if empty after deletion.
            val srcTiles = File(srcDir, "tiles")
            if (srcTiles.exists() && srcTiles.isDirectory) srcTiles.deleteRecursively()

        } catch (e: Exception) {
            val msg = "Migration failed: ${e.message}"
            Log.e(TAG, msg, e)
            prefs.migrationInProgress = false
            onState(MigrationState.Error(msg))
            return false
        }

        prefs.migrationInProgress = false
        onState(MigrationState.Complete)
        return true
    }

    /** Returns all data files to migrate, in a deterministic order. */
    private fun collectFiles(srcDir: File): List<File> {
        val result = mutableListOf<File>()
        for (name in DB_NAMES) {
            val f = File(srcDir, name)
            if (f.exists() && f.isFile) result += f
        }
        val tilesDir = File(srcDir, "tiles")
        if (tilesDir.exists() && tilesDir.isDirectory) {
            tilesDir.walkTopDown().filter { it.isFile }.sortedBy { it.path }.forEach { result += it }
        }
        return result
    }
}
