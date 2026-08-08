package sh.tutti.mobile

import android.app.Activity
import android.app.DownloadManager
import android.content.pm.PackageInstaller
import java.io.File
import java.security.MessageDigest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class MobileUpdateDownloaderTest {
    @get:Rule val temporaryFolder = TemporaryFolder()

    @Test
    fun `accepts a bounded HTTPS artifact request`() {
        val request =
            validateMobileUpdateArtifactRequest(
                "https://updates.example.test/app.apk",
                "a".repeat(64),
                1024.0,
            )

        assertEquals(1024L, request.expectedSizeBytes)
        assertEquals("tutti-update-${"a".repeat(64)}.apk", request.fileName)
    }

    @Test(expected = MobileUpdateDownloadFailure::class)
    fun `rejects HTTP update URLs`() {
        validateMobileUpdateArtifactRequest(
            "http://updates.example.test/app.apk",
            "a".repeat(64),
            1024.0,
        )
    }

    @Test(expected = MobileUpdateDownloadFailure::class)
    fun `rejects update URLs with embedded credentials`() {
        validateMobileUpdateArtifactRequest(
            "https://user:secret@updates.example.test/app.apk",
            "a".repeat(64),
            1024.0,
        )
    }

    @Test(expected = MobileUpdateDownloadFailure::class)
    fun `rejects oversized artifacts`() {
        validateMobileUpdateArtifactRequest(
            "https://updates.example.test/app.apk",
            "a".repeat(64),
            (MAX_MOBILE_UPDATE_BYTES + 1).toDouble(),
        )
    }

    @Test
    fun `verifies artifact size and checksum`() {
        val content = "verified apk".toByteArray()
        val file = File(temporaryFolder.root, "app.apk").apply { writeBytes(content) }
        val sha256 =
            MessageDigest.getInstance("SHA-256").digest(content).joinToString("") { byte ->
                "%02x".format(byte.toInt() and 0xff)
            }
        val request =
            validateMobileUpdateArtifactRequest(
                "https://updates.example.test/app.apk",
                sha256,
                content.size.toDouble(),
            )

        verifyMobileUpdateArtifact(file, request)
    }

    @Test
    fun `rejects an artifact whose size differs from the manifest`() {
        val file = File(temporaryFolder.root, "wrong-size.apk").apply { writeText("apk") }
        val request =
            validateMobileUpdateArtifactRequest(
                "https://updates.example.test/app.apk",
                "a".repeat(64),
                4.0,
            )

        val failure =
            assertThrows(MobileUpdateDownloadFailure::class.java) {
                verifyMobileUpdateArtifact(file, request)
            }

        assertEquals("UPDATE_SIZE_MISMATCH", failure.code)
    }

    @Test
    fun `rejects an artifact whose checksum differs from the manifest`() {
        val file = File(temporaryFolder.root, "wrong-sha.apk").apply { writeText("apk") }
        val request =
            validateMobileUpdateArtifactRequest(
                "https://updates.example.test/app.apk",
                "a".repeat(64),
                file.length().toDouble(),
            )

        val failure =
            assertThrows(MobileUpdateDownloadFailure::class.java) {
                verifyMobileUpdateArtifact(file, request)
            }

        assertEquals("UPDATE_CHECKSUM_FAILED", failure.code)
    }

    @Test
    fun `classifies package installer cancellation separately from failure`() {
        val cancelled =
            classifyMobileUpdateInstallOutcome(
                Activity.RESULT_CANCELED,
                PackageInstaller.STATUS_FAILURE_ABORTED,
            )
        val conflict =
            classifyMobileUpdateInstallOutcome(
                Activity.RESULT_CANCELED,
                PackageInstaller.STATUS_FAILURE_CONFLICT,
            )

        assertEquals(MobileUpdateInstallOutcomeKind.CANCELLED, cancelled.kind)
        assertEquals(MobileUpdateInstallOutcomeKind.FAILED, conflict.kind)
        assertEquals("UPDATE_INSTALL_CONFLICT", conflict.errorCode)
    }

    @Test
    fun `maps system download reasons to actionable failures`() {
        assertEquals(
            "UPDATE_STORAGE_INSUFFICIENT",
            mobileUpdateDownloadFailureForReason(
                DownloadManager.ERROR_INSUFFICIENT_SPACE,
            ).code,
        )
        assertEquals(
            "UPDATE_DOWNLOAD_FILE_FAILED",
            mobileUpdateDownloadFailureForReason(DownloadManager.ERROR_FILE_ERROR).code,
        )
        assertEquals(
            "UPDATE_DOWNLOAD_SERVER_FAILED",
            mobileUpdateDownloadFailureForReason(
                DownloadManager.ERROR_UNHANDLED_HTTP_CODE,
            ).code,
        )
    }
}
