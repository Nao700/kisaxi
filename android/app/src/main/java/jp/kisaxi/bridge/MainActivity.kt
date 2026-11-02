package jp.kisaxi.bridge

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView

/** 権限を取ってサービスを起動し、管理画面 (http://127.0.0.1:8073) を開く。「停止」でキルスイッチ。 */
class MainActivity : Activity() {
    private lateinit var status: TextView

    override fun onCreate(b: Bundle?) {
        super.onCreate(b)
        val box = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(48, 48, 48, 48) }
        box.addView(TextView(this).apply {
            text = "KISAXI Bluetooth ブリッジ\nAndroid の設定で無線機をペアリングしてから、管理画面で機器を選びます。"
        })
        status = TextView(this).apply { setPadding(0, 32, 0, 32) }
        box.addView(status)
        box.addView(Button(this).apply { text = "起動して管理画面を開く"; setOnClickListener { start() } })
        box.addView(Button(this).apply { text = "管理画面を開く"; setOnClickListener { openUi() } })
        box.addView(Button(this).apply { text = "電池の最適化から除外する (止まりにくくなる)"; setOnClickListener { ignoreBattery() } })
        box.addView(Button(this).apply { text = "停止 (キルスイッチ)"; setOnClickListener { stop() } })
        setContentView(box)
    }

    override fun onResume() { super.onResume(); refresh() }

    private fun refresh() {
        val ign = getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(packageName)
        status.text = (if (BridgeService.running) "状態: 動作中" else "状態: 停止中") +
            "\n電池の最適化: " + (if (ign) "除外済み" else "対象 (バックグラウンドで止まることがあります)")
    }

    private fun start() {
        val perms = ArrayList<String>()
        if (Build.VERSION.SDK_INT >= 31) perms.add(Manifest.permission.BLUETOOTH_CONNECT)
        if (Build.VERSION.SDK_INT >= 33) perms.add(Manifest.permission.POST_NOTIFICATIONS)
        if (perms.isNotEmpty()) requestPermissions(perms.toTypedArray(), 1)
        startForegroundService(Intent(this, BridgeService::class.java))
        window.decorView.postDelayed({ refresh(); openUi() }, 800)
    }

    private fun stop() {
        startService(Intent(this, BridgeService::class.java).setAction(BridgeService.ACTION_STOP))
        window.decorView.postDelayed({ refresh() }, 500)
    }

    private fun openUi() = startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("http://127.0.0.1:8073/")))

    @android.annotation.SuppressLint("BatteryLife")
    private fun ignoreBattery() {
        startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName")))
    }
}
