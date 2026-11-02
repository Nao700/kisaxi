package jp.kisaxi.bridge

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView

/** 権限を取ってサービスを起動し、管理画面 (http://127.0.0.1:8073) をブラウザで開くだけの最小画面。 */
class MainActivity : Activity() {
    override fun onCreate(b: Bundle?) {
        super.onCreate(b)
        val box = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(48, 48, 48, 48) }
        box.addView(TextView(this).apply {
            text = "KISAXI Bluetooth ブリッジ\nAndroid の設定で無線機をペアリングしてから、管理画面で機器を選びます。"
        })
        box.addView(Button(this).apply {
            text = "起動して管理画面を開く"
            setOnClickListener { start() }
        })
        setContentView(box)
    }

    private fun start() {
        if (Build.VERSION.SDK_INT >= 31) {
            requestPermissions(arrayOf(Manifest.permission.BLUETOOTH_CONNECT, Manifest.permission.POST_NOTIFICATIONS), 1)
        } else if (Build.VERSION.SDK_INT >= 33) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1)
        }
        startForegroundService(Intent(this, BridgeService::class.java))
        startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("http://127.0.0.1:8073/")))
    }
}
