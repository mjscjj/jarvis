use std::io::{BufRead, BufReader, Read};
use std::sync::mpsc::{self, RecvTimeoutError};
use std::thread;
use std::time::{Duration, Instant};

const CONNECTION_PREFIX: &str = "JARVIS_RUNTIME_CONNECTION ";

// Both readers own a sender: disconnect only after stderr has been drained,
// so stdout closing cannot hide the supervisor's final error message.
pub fn read_connection(
    stdout: impl Read + Send + 'static,
    stderr: impl Read + Send + 'static,
    timeout: Duration,
) -> Result<String, String> {
    let (sender, receiver) = mpsc::channel();
    for (stream, is_error) in [
        (Box::new(stdout) as Box<dyn Read + Send>, false),
        (Box::new(stderr) as Box<dyn Read + Send>, true),
    ] {
        let sender = sender.clone();
        thread::spawn(move || {
            for line in BufReader::new(stream).lines() {
                match line {
                    Ok(line) => {
                        let is_connection = !is_error && line.starts_with(CONNECTION_PREFIX);
                        if !is_connection {
                            eprintln!("[jarvis-runtime] {line}");
                        }
                        // Continue draining after the startup receiver is dropped.
                        let _ = sender.send((is_error, line));
                    }
                    Err(error) => {
                        let _ = sender.send((true, format!("读取服务输出失败：{error}")));
                        break;
                    }
                }
            }
        });
    }
    drop(sender);
    let deadline = Instant::now() + timeout;
    let mut diagnostics = Vec::new();
    loop {
        match receiver.recv_timeout(deadline.saturating_duration_since(Instant::now())) {
            Ok((false, line)) if line.starts_with(CONNECTION_PREFIX) => {
                return Ok(line[CONNECTION_PREFIX.len()..].to_string());
            }
            Ok((_, line)) => diagnostics.push(line),
            Err(error) => {
                let reason = match error {
                    RecvTimeoutError::Timeout => {
                        format!("Go 服务未在 {} 秒内就绪", timeout.as_secs())
                    }
                    RecvTimeoutError::Disconnected => "Go 服务在返回连接信息前关闭了输出".into(),
                };
                if diagnostics.is_empty() {
                    return Err(format!("{reason}，未收到诊断输出"));
                }
                return Err(format!("{reason}\n\n{}", diagnostics.join("\n")));
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;

    #[test]
    fn empty_stdout_preserves_final_stderr() {
        for _ in 0..100 {
            let error = read_connection(
                Cursor::new(""),
                Cursor::new("jarvis-app-service: required bundled resource missing\n"),
                Duration::from_secs(1),
            )
            .unwrap_err();
            assert!(error.contains("required bundled resource missing"), "{error}");
        }
    }

    #[test]
    fn returns_connection_despite_startup_logs() {
        let result = read_connection(
            Cursor::new("starting\nJARVIS_RUNTIME_CONNECTION {\"httpUrl\":\"http://127.0.0.1:18800\"}\n"),
            Cursor::new("diagnostic\n"),
            Duration::from_secs(1),
        )
        .unwrap();
        assert_eq!(result, "{\"httpUrl\":\"http://127.0.0.1:18800\"}");
    }

    #[test]
    fn stderr_cannot_announce_connection() {
        let result = read_connection(
            Cursor::new(""),
            Cursor::new("JARVIS_RUNTIME_CONNECTION {}\n"),
            Duration::from_secs(1),
        );
        assert!(result.is_err());
    }
}
