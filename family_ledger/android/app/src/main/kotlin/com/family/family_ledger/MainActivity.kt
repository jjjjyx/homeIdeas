package com.family.family_ledger

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import io.flutter.embedding.android.FlutterFragmentActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File

/**
 * 接收系统分享过来的 CSV（AI 智能体把消费截图转成表格后分享到本 App）。
 *
 * 之所以自己实现而不是用第三方插件：插件把 text/csv 归类成 TEXT，
 * 会把「文件路径字符串」当成内容传给 Dart，服务端解析必然失败。
 * 这里直接用 ContentResolver 把流复制到应用私有缓存，Dart 侧拿到的就是真实内容。
 */
class MainActivity : FlutterFragmentActivity() {

    private var channel: MethodChannel? = null
    private var pendingPath: String? = null
    private var pendingText: String? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        channel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, CHANNEL).also { ch ->
            ch.setMethodCallHandler { call, result ->
                when (call.method) {
                    "getInitialShare" -> {
                        result.success(currentPayload())
                        clearPending()
                    }
                    // 设备唯一标识：同一台手机重装后仍是同一个值，
                    // 用于后台识别「这是同一台设备」，避免重复授权。
                    "getDeviceId" -> {
                        result.success(deviceId())
                    }
                    else -> result.notImplemented()
                }
            }
        }
        // App 冷启动时可能带着分享内容
        readShare(intent)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        readShare(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        readShare(intent)
    }

    private fun readShare(intent: Intent?) {
        if (intent == null) return
        if (intent.action != Intent.ACTION_SEND && intent.action != Intent.ACTION_VIEW) return

        val uri: Uri? = getStreamUri(intent)
        val text: String? = intent.getStringExtra(Intent.EXTRA_TEXT)

        if (uri != null) {
            try {
                val name = guessName(intent.type)
                val target = File(cacheDir, "share_${System.currentTimeMillis()}_$name")
                contentResolver.openInputStream(uri)?.use { input ->
                    target.outputStream().use { output -> input.copyTo(output) }
                }
                if (target.exists() && target.length() > 0) {
                    pendingPath = target.absolutePath
                    pendingText = null
                    notifyDart()
                    return
                }
            } catch (e: Exception) {
                // 读不到就退回按文本处理，并把原因带给 Dart 方便排查
                pendingText = "__SHARE_ERROR__:" + (e.message ?: e.javaClass.simpleName)
            }
        }

        if (!text.isNullOrBlank()) {
            pendingText = text
            pendingPath = null
            notifyDart()
        }
    }

    private fun getStreamUri(intent: Intent): Uri? {
        val extra = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableExtra(Intent.EXTRA_STREAM)
        }
        if (extra != null) return extra
        return if (intent.action == Intent.ACTION_VIEW) intent.data else null
    }

    private fun guessName(mime: String?): String = when {
        mime?.contains("excel", true) == true -> "import.xlsx"
        mime?.contains("csv", true) == true -> "import.csv"
        else -> "import.csv"
    }

    private fun currentPayload(): Map<String, String?> = mapOf(
        "path" to pendingPath,
        "text" to pendingText
    )

    private fun clearPending() {
        pendingPath = null
        pendingText = null
    }

    /**
     * 设备唯一标识。
     *
     * 优先 ANDROID_ID：同一台手机 + 同一签名 + 同一用户下保持不变，
     * 卸载重装不会变，恢复出厂设置才会变——正好符合「同一台设备」的语义。
     * 但部分国产 ROM 会返回固定值或空，这时回退到拼接信息。
     */
    private fun deviceId(): String {
        val androidId = try {
            android.provider.Settings.Secure.getString(contentResolver, android.provider.Settings.Secure.ANDROID_ID)
        } catch (e: Exception) {
            null
        }
        if (!androidId.isNullOrBlank() && androidId != "9774d56d682e549c") {
            return "aid:$androidId"
        }
        val fallback = "${android.os.Build.MANUFACTURER}-${android.os.Build.MODEL}-${android.os.Build.FINGERPRINT}"
        return "fp:" + fallback.replace(" ", "_")
    }

    private fun notifyDart() {
        channel?.invokeMethod("onShared", currentPayload())
    }

    companion object {
        private const val CHANNEL = "family_ledger/share"
    }
}
