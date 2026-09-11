use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::thread;
use std::time::{Duration, Instant};

use serde::Deserialize;
use tauri::Manager;
use tauri_plugin_updater::UpdaterExt;

mod runtime_output;

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

    let connection_result = (|| {
        let stdout = child.stdout.take().ok_or("Go 服务没有提供启动输出")?;
        let stderr = child.stderr.take().ok_or("Go 服务没有提供诊断输出")?;
        // Go waits up to 30 seconds for each service, then cleans up on error.
        let raw = runtime_output::read_connection(stdout, stderr, Duration::from_secs(75))?;
        serde_json::from_str::<RuntimeConnection>(&raw)
            .map_err(|error| format!("Go 服务返回了无效连接信息：{error}"))
    })();
    let connection = match connection_result {
        Ok(connection) => connection,
        Err(error) => {
            let status = child.try_wait().ok().flatten();
            stop_runtime(&mut child);
            return Err(match status {
                Some(status) => format!("{error}\n退出状态：{status}"),
                None => error,
            });
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
    stop_runtime(&mut child);
}

fn stop_runtime(child: &mut Child) {
    if matches!(child.try_wait(), Ok(Some(_))) {
        return;
    }
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
            "document.body.innerHTML='<main style=\"font:14px -apple-system;padding:32px;color:#202428\"><h2>Jarvis 启动失败</h2><pre style=\"white-space:pre-wrap\"></pre></main>'; document.querySelector('pre').textContent={};",
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
                    Ok(Some(status)) => {
                        child.take();
                        Some(format!("本地服务意外退出：{status}"))
                    }
                    Err(error) => {
                        // Keep the child handle so quitting still cleans it up.
                        Some(format!("无法读取本地服务状态：{error}"))
                    }
                    Ok(None) => None,
                },
                None => return,
            }
        };
        if let Some(error) = exited {
            show_startup_error(&app, &format!(
                "{error}\n请查看 ~/Library/Application Support/Jarvis/logs 中的日志，修正问题后退出并重新打开 Jarvis。数据无需删除。"
            ));
            return;
        }
    });
}

fn check_for_update(app: tauri::AppHandle) {
    if cfg!(debug_assertions) {
        return;
    }
    tauri::async_runtime::spawn(async move {
        let update = match app.updater() {
            Ok(updater) => match updater.check().await {
                Ok(update) => update,
                Err(error) => {
                    eprintln!("jarvis-desktop: update check failed: {error}");
                    return;
                }
            },
            Err(error) => {
                eprintln!("jarvis-desktop: initialize updater failed: {error}");
                return;
            }
        };
        let Some(update) = update else {
            return;
        };
        eprintln!(
            "jarvis-desktop: installing update {} -> {}",
            app.package_info().version,
            update.version
        );
        if let Err(error) = update.download_and_install(|_, _| {}, || {}).await {
            eprintln!("jarvis-desktop: update installation failed: {error}");
            return;
        }
        app.restart();
    });
}

fn main() {
    let application = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.set_focus();
            }
        }))
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .manage(RuntimeState::default())
        .setup(|app| {
            match spawn_runtime(app.handle()) {
                Ok(child) => {
                    *app.state::<RuntimeState>().child.lock().unwrap() = Some(child);
                    monitor_runtime(app.handle().clone());
                    check_for_update(app.handle().clone());
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
