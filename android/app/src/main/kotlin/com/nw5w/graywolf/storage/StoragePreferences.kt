package com.nw5w.graywolf.storage

import android.content.Context
import android.os.Environment
import java.io.File

class StoragePreferences(private val ctx: Context) {
    private val prefs = ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    var useSDCard: Boolean
        get() = prefs.getBoolean(KEY_USE_SD_CARD, false)
        set(value) = prefs.edit().putBoolean(KEY_USE_SD_CARD, value).apply()

    // Set before migration begins and cleared on success; lets the Service
    // detect a partially-completed migration on the next cold start.
    var migrationInProgress: Boolean
        get() = prefs.getBoolean(KEY_MIGRATION_IN_PROGRESS, false)
        set(value) = prefs.edit().putBoolean(KEY_MIGRATION_IN_PROGRESS, value).apply()

    /** Returns the removable SD card external files dir, or null if none is mounted. */
    fun getSdCardDir(): File? {
        val dirs = ctx.getExternalFilesDirs(null) ?: return null
        return dirs.firstOrNull { dir ->
            dir != null &&
                !Environment.isExternalStorageEmulated(dir) &&
                Environment.isExternalStorageRemovable(dir) &&
                Environment.getExternalStorageState(dir) == Environment.MEDIA_MOUNTED
        }
    }

    /** Returns the directory where data files should currently live. */
    fun getDataDir(): File {
        if (useSDCard) {
            val sd = getSdCardDir()
            if (sd != null) return sd
        }
        return ctx.filesDir
    }

    companion object {
        private const val PREFS_NAME = "graywolf_storage"
        private const val KEY_USE_SD_CARD = "use_sd_card"
        private const val KEY_MIGRATION_IN_PROGRESS = "migration_in_progress"
    }
}
