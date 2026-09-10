use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::thread;
use std::time::{Duration, Instant};

use serde::Deserialize;
use tauri::Manager;

const CONNECTION_PREFIX: &str = "JARVIS_RUNTIME_CONNECTION ";

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct RuntimeConnection {
    http_url: String,
}

#[derive(Default)]
struct RuntimeState {
    child: Mutex<Option<Child>>,
}

fn runtime_root(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    if cfg!(debug_assertions) {
        return Ok(PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("generated")
            .join("runtime"));
    }
    if let Ok(path) = app.path().resource_dir() {
        return Ok(path.join("runtime"));
    }
    let executable =
        std::env::current_exe().map_err(|error| format!("无法定位应用程序：{error}"))?;
    let contents = executable
        .parent()
        .and_then(|macos| macos.parent())
        .ok_or_else(|| format!("无法从 {} 定位应用资源", executable.display()))?;
    Ok(contents.join("Resources").join("runtime"))
}

fn spawn_runtime(app: &tauri::AppHandle) -> Result<Child, String> {
    let root = runtime_root(app)?;
    let binary = root.join("bin").join("jarvis-app-service");
    if !binary.is_file() {
        return Err(format!("缺少 Go 启动服务：{}", binary.display()));
    }
    let mut command = Command::new(&binary);
    command
        .args([
            "--resources",
            root.to_string_lossy().as_ref(),
            "--supervisor-pid",
            &std::process::id().to_string(),
        ])
        .current_dir(&root)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    let mut child = command
        .spawn()
        .map_err(|error| format!("启动 Go 服务失败：{error}"))?;

    if let Some(stderr) = child.stderr.take() {
        thread::spawn(move || {
            for line in BufReader::new(stderr).lines().map_while(Result::ok) {
                eprintln!("[jarvis-runtime] {line}");
            }
        });
    }

    let stdout = child
        .stdout
        .take()
        .ok_or_else(|| "Go 服务没有提供启动输出".to_string())?;
    let (sender, receiver) = std::sync::mpsc::channel();
    thread::spawn(move || {
        for line in BufReader::new(stdout).lines().map_while(Result::ok) {
            if let Some(raw) = line.strip_prefix(CONNECTION_PREFIX) {
                let _ = sender.send(raw.to_string());
            } else {
                println!("[jarvis-runtime] {line}");
            }
        }
    });

    let deadline = Instant::now() + Duration::from_secs(60);
    let connection = loop {
        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            let _ = child.kill();
            return Err("Go 服务未在 60 秒内就绪".into());
        }
        match receiver.recv_timeout(remaining.min(Duration::from_millis(250))) {
            Ok(raw) => {
                break serde_json::from_str::<RuntimeConnection>(&raw)
                    .map_err(|error| format!("Go 服务返回了无效连接信息：{error}"))?;
            }
            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {
                if let Ok(Some(status)) = child.try_wait() {
                    return Err(format!("Go 服务启动失败：{status}"));
                }
            }
            Err(error) => return Err(format!("读取 Go 服务连接信息失败：{error}")),
        }
    };

    let url = connection
        .http_url
        .parse()
        .map_err(|error| format!("Go 服务地址无效：{error}"))?;
    let window = app
        .get_webview_window("main")
        .ok_or_else(|| "找不到主窗口".to_string())?;
    window
        .navigate(url)
        .map_err(|error| format!("打开 Jarvis 页面失败：{error}"))?;
    window
        .show()
        .map_err(|error| format!("显示 Jarvis 窗口失败：{error}"))?;
    window
        .set_focus()
        .map_err(|error| format!("聚焦 Jarvis 窗口失败：{error}"))?;
    Ok(child)
}

fn shutdown_runtime(state: &RuntimeState) {
    let Some(mut child) = state.child.lock().unwrap().take() else {
        return;
    };
    #[cfg(unix)]
    unsafe {
        libc::kill(child.id() as i32, libc::SIGTERM);
    }
    #[cfg(not(unix))]
    let _ = child.kill();

    let deadline = Instant::now() + Duration::from_secs(20);
    while Instant::now() < deadline {
        match child.try_wait() {
            Ok(Some(_)) => return,
            Ok(None) => thread::sleep(Duration::from_millis(100)),
            Err(_) => break,
        }
    }
    let _ = child.kill();
    let _ = child.wait();
}

fn show_startup_error(app: &tauri::AppHandle, error: &str) {
    eprintln!("jarvis-desktop: {error}");
    if let Some(window) = app.get_webview_window("main") {
        let message = serde_json::to_string(error).unwrap_or_else(|_| "\"启动失败\"".into());
        let script = format!(
            "document.body.innerHTML='<main style=\"font:14px -apple-system;padding:32px;color:#202428\"><h2>Jarvis 启动失败</h2><pre style=\"white-space:pre-wrap\">'+{}+'</pre></main>';",
            message
        );
        let _ = window.eval(&script);
        let _ = window.show();
    }
}

fn monitor_runtime(app: tauri::AppHandle) {
    thread::spawn(move || loop {
        thread::sleep(Duration::from_millis(250));
        let exited = {
            let state = app.state::<RuntimeState>();
            let mut child = state.child.lock().unwrap();
            match child.as_mut() {
                Some(process) => match process.try_wait() {
                    Ok(Some(_)) | Err(_) => {
                        child.take();
                        true
                    }
                    Ok(None) => false,
                },
                None => true,
            }
        };
        if exited {
            app.exit(0);
            return;
        }
    });
}

fn main() {
    let application = tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .manage(RuntimeState::default())
        .setup(|app| {
            match spawn_runtime(app.handle()) {
                Ok(child) => {
                    *app.state::<RuntimeState>().child.lock().unwrap() = Some(child);
                    monitor_runtime(app.handle().clone());
                }
                Err(error) => show_startup_error(app.handle(), &error),
            }
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("build Jarvis desktop application");

    application.run(|app, event| match event {
        tauri::RunEvent::ExitRequested { .. } | tauri::RunEvent::Exit => {
            shutdown_runtime(&app.state::<RuntimeState>());
        }
        _ => {}
    });
}
