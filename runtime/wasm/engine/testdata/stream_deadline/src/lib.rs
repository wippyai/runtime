wit_bindgen::generate!({ path: "../tcp/wit", world: "tcp", generate_all });

use wasi::io::streams::StreamError;
use wasi::sockets::network::{ErrorCode, IpAddressFamily, IpSocketAddress, Ipv4SocketAddress};

struct Fixture;

fn timeout(result: Result<(), StreamError>) -> Result<(), String> {
    match result {
        Err(StreamError::LastOperationFailed(error)) => {
            let detail = error.to_debug_string();
            if !detail.contains("deadline") && !detail.contains("timed out") {
                return Err(format!("missing timeout detail: {detail}"));
            }
            Ok(())
        }
        Err(StreamError::Closed) => Err("timeout was reported as clean close".into()),
        Ok(()) => Err("stalled operation unexpectedly completed".into()),
    }
}

impl Guest for Fixture {
    fn run() -> Result<String, String> {
        let network = wasi::sockets::instance_network::instance_network();
        let socket = wasi::sockets::tcp_create_socket::create_tcp_socket(IpAddressFamily::Ipv4)
            .map_err(|e| format!("socket: {e:?}"))?;
        socket.start_connect(&network, IpSocketAddress::Ipv4(Ipv4SocketAddress {
            address: (127, 0, 0, 1), port: 8099,
        })).map_err(|e| format!("connect: {e:?}"))?;
        let (input, output) = loop {
            match socket.finish_connect() {
                Ok(streams) => break streams,
                Err(ErrorCode::WouldBlock) => socket.subscribe().block(),
                Err(e) => return Err(format!("finish-connect: {e:?}")),
            }
        };
        input.subscribe().block();
        let mode = input.read(1).map_err(|e| format!("mode: {e:?}"))?;
        if mode.len() != 1 { return Err("missing mode".into()); }
        let result = match mode[0] {
            b'R' => { timeout(input.blocking_read(4).map(|_| ()))?; "timed-out:closed".into() }
            b'K' => { timeout(input.blocking_skip(4).map(|_| ()))?; "timed-out:closed".into() }
            b'W' => { timeout(output.blocking_write_and_flush(b"ping"))?; "timed-out:closed".into() }
            b'Z' => { timeout(output.blocking_write_zeroes_and_flush(4))?; "timed-out:closed".into() }
            b'F' => {
                if output.check_write().map_err(|e| format!("permit: {e:?}"))? < 4 {
                    return Err("missing initial write permit".into());
                }
                output.write(b"ping").map_err(|e| format!("write: {e:?}"))?;
                timeout(output.blocking_flush())?;
                "timed-out:closed".into()
            }
            b'S' => {
                // Fill the bounded output ring while the peer refuses to read.
                let permit = output.check_write().map_err(|e| format!("permit: {e:?}"))?;
                if permit == 0 || permit > 65536 { return Err("unexpected ring capacity".into()); }
                output.write(&vec![b'x'; permit as usize]).map_err(|e| format!("fill: {e:?}"))?;
                timeout(output.blocking_splice(&input, 4).map(|_| ()))?;
                "timed-out:closed".into()
            }
            b'I' => {
                // Generic subscription waits must outlive socket operation timeouts.
                input.subscribe().block();
                let data = input.blocking_read(4).map_err(|e| format!("idle read: {e:?}"))?;
                if data != b"done" { return Err(format!("idle payload: {data:?}")); }
                "idle:done".into()
            }
            b'D' => {
                output.blocking_write_and_flush(b"ping").map_err(|e| format!("delayed success: {e:?}"))?;
                "written".into()
            }
            _ => return Err("unknown mode".into()),
        };
        if mode[0] != b'I' && mode[0] != b'D' {
            if !matches!(input.read(1), Err(StreamError::Closed)) {
                return Err("input remained open after socket timeout".into());
            }
            if !matches!(output.check_write(), Err(StreamError::Closed)) {
                return Err("output remained open after socket timeout".into());
            }
        }
        drop(input);
        drop(output);
        drop(socket);
        drop(network);
        Ok(result)
    }
}
export!(Fixture);
