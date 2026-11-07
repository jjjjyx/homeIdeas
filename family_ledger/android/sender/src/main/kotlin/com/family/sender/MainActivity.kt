package com.family.sender

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import androidx.core.content.FileProvider
import android.app.Activity
import java.io.File

/**
 * 测试用的「分享发送方」：生成一份符合记账本固定格式的 CSV，
 * 通过 FileProvider + ACTION_SEND 分享给「家庭记账本」。
 *
 * 存在的意义：验证跨应用的 content:// URI 授权读链路，
 * adb 注入的 intent 无法携带这个授权。
 */
class MainActivity : Activity() {

    // 每次分享都用当天日期 + 随机金额，方便反复演示
    private fun buildCsv(): String {
        val day = java.text.SimpleDateFormat("yyyy-MM-dd", java.util.Locale.US).format(java.util.Date())
        fun r(base: Double) = String.format(java.util.Locale.US, "%.2f", base + (0..500).random() / 100.0)
        return "日期,类型,金额,分类,归属,备注\n" +
            "$day,支出,${r(30.0)},餐饮,家庭,楼下面馆-午饭\n" +
            "$day,支出,${r(6.0)},交通,家庭,地铁-通勤\n" +
            "$day,支出,${r(190.0)},工作,个人,DeepSeek-API充值\n" +
            "$day,支出,${r(20.0)},医疗,家庭,药店-感冒药"
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 96, 48, 48)
        }
        layout.addView(TextView(this).apply {
            text = "点击下面的按钮，把测试 CSV 分享给「家庭记账本」"
            textSize = 16f
        })
        layout.addView(TextView(this).apply {
            text = buildCsv()
            textSize = 12f
            setPadding(0, 32, 0, 32)
        })
        layout.addView(Button(this).apply {
            text = "分享 CSV 文件"
            setOnClickListener { shareFile() }
        })
        layout.addView(Button(this).apply {
            text = "分享 CSV 文本"
            setOnClickListener { shareText() }
        })
        setContentView(layout)
    }

    private fun writeFile(): File {
        val file = File(cacheDir, "bill_${System.currentTimeMillis()}.csv")
        file.writeText(buildCsv(), Charsets.UTF_8)
        return file
    }

    private fun shareFile() {
        val file = writeFile()
        val uri: Uri = FileProvider.getUriForFile(this, "com.family.sender.fileprovider", file)
        val intent = Intent(Intent.ACTION_SEND).apply {
            type = "text/csv"
            putExtra(Intent.EXTRA_STREAM, uri)
            putExtra(Intent.EXTRA_SUBJECT, "测试账单")
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        startActivity(Intent.createChooser(intent, "分享到"))
    }

    private fun shareText() {
        val intent = Intent(Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(Intent.EXTRA_TEXT, buildCsv())
        }
        startActivity(Intent.createChooser(intent, "分享到"))
    }
}