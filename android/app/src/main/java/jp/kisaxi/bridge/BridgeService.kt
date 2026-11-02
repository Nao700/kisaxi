package jp.kisaxi.bridge

import android.annotation.SuppressLint
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.net.wifi.WifiManager
import android.os.PowerManager
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothSocket
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.util.UUID
import kotlin.concurrent.thread

/**
 * Go 製の KISAXI を起動し、Bluetooth (SPP) と KISS TCP を中継するサービス。
 *
 *  HTTP 127.0.0.1:8074  GET /devices, POST /connect?addr=, POST /disconnect   (Go の src/btbridge.go が呼ぶ)
 *  TCP  127.0.0.1:8075  KISS (Bluetooth SPP との双方向中継)
 */
class BridgeService : Service() {
    private val spp = UUID.fromString("00001101-0000-1000-8000-00805F9B34FB")
    private val kissPort = 8075
    private var bt: BluetoothSocket? = null
    private var kissServer: ServerSocket? = null
    private var goProc: Process? = null
    private var httpServer: ServerSocket? = null
    private var wake: PowerManager.WakeLock? = null
    private var wifi: WifiManager.WifiLock? = null

    companion object {
        const val ACTION_STOP = "jp.kisaxi.bridge.STOP"
        @Volatile var running = false
    }

    override fun onBind(i: Intent?): IBinder? = null

    override fun onStartCommand(i: Intent?, f: Int, id: Int): Int {
        if (i?.action == ACTION_STOP) { // キルスイッチ: 通知の「停止」ボタンと画面の「停止」
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
            return START_NOT_STICKY
        }
        if (running) return START_STICKY
        running = true
        // 画面オフ中も CPU と Wi-Fi を止めない (APRS-IS / Tailscale の接続維持)
        wake = getSystemService(PowerManager::class.java).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "kisaxi:bridge").apply { acquire() }
        wifi = (applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager)
            .createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "kisaxi:bridge").apply { acquire() }
        val ch = NotificationChannel("kisaxi", "KISAXI", NotificationManager.IMPORTANCE_LOW)
        getSystemService(NotificationManager::class.java).createNotificationChannel(ch)
        val n = Notification.Builder(this, "kisaxi").setContentTitle("KISAXI 動作中")
            .setSmallIcon(android.R.drawable.stat_sys_data_bluetooth)
            .addAction(Notification.Action.Builder(null, "停止",
                PendingIntent.getService(this, 0, Intent(this, BridgeService::class.java).setAction(ACTION_STOP),
                    PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)).build())
            .build()
        startForeground(1, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE)
        thread { httpLoop() }
        thread { startGo() }
        return START_STICKY
    }

    override fun onDestroy() {
        running = false
        closeBt()
        try { kissServer?.close() } catch (_: Exception) {}
        try { httpServer?.close() } catch (_: Exception) {}
        goProc?.destroy()
        if (wake?.isHeld == true) wake?.release()
        if (wifi?.isHeld == true) wifi?.release()
    }

    /** Go を起動し、落ちたら 3 秒後に再起動する (停止されるまで)。 */
    private fun startGo() {
        val bin = File(applicationInfo.nativeLibraryDir, "libkisaxi.so")
        val cfg = File(filesDir, "aprsigate.json")
        while (running) {
            try {
                // -listen 0.0.0.0: Tailscale 経由で別端末のブラウザから開けるようにする。LAN に出したくなければ tailnet の IP を指定する。
                goProc = ProcessBuilder(bin.path, "-config", cfg.path, "-listen", "0.0.0.0:8073", "-nobrowser")
                    .redirectErrorStream(true).start()
                goProc!!.inputStream.bufferedReader().forEachLine { android.util.Log.i("kisaxi", it) }
                goProc!!.waitFor()
            } catch (e: Exception) { android.util.Log.e("kisaxi", "go failed", e) }
            if (running) Thread.sleep(3000)
        }
    }

    @SuppressLint("MissingPermission")
    private fun devices(): JSONArray {
        val arr = JSONArray()
        BluetoothAdapter.getDefaultAdapter()?.bondedDevices?.forEach {
            arr.put(JSONObject().put("name", it.name ?: it.address).put("addr", it.address))
        }
        return arr
    }

    private fun closeBt() { try { bt?.close() } catch (_: Exception) {}; bt = null }

    @SuppressLint("MissingPermission")
    private fun connect(addr: String): String {
        val adapter = BluetoothAdapter.getDefaultAdapter() ?: error("Bluetooth が使えません")
        val dev = adapter.bondedDevices.firstOrNull { it.address == addr } ?: error("ペアリング済みの機器ではありません")
        closeBt()
        adapter.cancelDiscovery()
        val s = dev.createRfcommSocketToServiceRecord(spp)
        s.connect()
        bt = s
        if (kissServer == null) kissServer = ServerSocket(kissPort, 1, InetAddress.getByName("127.0.0.1"))
        thread { relay(s) }
        return "127.0.0.1:$kissPort"
    }

    /** KISS TCP の接続を 1 本受け、Bluetooth ソケットと双方向にコピーする。 */
    private fun relay(s: BluetoothSocket) {
        try {
            val c: Socket = kissServer!!.accept()
            val t = thread { try { c.getInputStream().copyTo(s.outputStream) } catch (_: Exception) {} }
            try { s.inputStream.copyTo(c.getOutputStream()) } catch (_: Exception) {}
            c.close(); t.join(500)
        } finally { if (bt === s) closeBt() }
    }

    /** 依存を増やさないための極小 HTTP サーバー (127.0.0.1 のみ)。 */
    private fun httpLoop() {
        val srv = ServerSocket(8074, 8, InetAddress.getByName("127.0.0.1"))
        httpServer = srv
        while (running) {
            val c = try { srv.accept() } catch (_: Exception) { break }
            thread {
                c.use {
                    val line = it.getInputStream().bufferedReader().readLine() ?: return@thread
                    val (method, target) = line.split(" ").let { p -> p[0] to p[1] }
                    val path = target.substringBefore('?')
                    val q = target.substringAfter('?', "")
                    var code = 200
                    val body = try {
                        when {
                            method == "GET" && path == "/devices" -> devices().toString()
                            method == "POST" && path == "/connect" -> {
                                val addr = q.split("&").firstOrNull { x -> x.startsWith("addr=") }
                                    ?.substringAfter("=")?.let { v -> java.net.URLDecoder.decode(v, "UTF-8") } ?: error("addr がありません")
                                JSONObject().put("ok", true).put("kiss", connect(addr)).toString()
                            }
                            method == "POST" && path == "/disconnect" -> { closeBt(); """{"ok":true}""" }
                            else -> { code = 404; """{"error":"not found"}""" }
                        }
                    } catch (e: Exception) { code = 500; JSONObject().put("error", e.message ?: "error").toString() }
                    val b = body.toByteArray()
                    it.getOutputStream().write(("HTTP/1.1 $code OK\r\nContent-Type: application/json\r\nContent-Length: ${b.size}\r\nConnection: close\r\n\r\n").toByteArray() + b)
                }
            }
        }
    }
}
