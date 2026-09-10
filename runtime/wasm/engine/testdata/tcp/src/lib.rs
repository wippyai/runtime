wit_bindgen::generate!({ path: "wit", world: "tcp", generate_all });

struct Fixture;

impl Guest for Fixture {
    fn run() -> Result<String, String> {
        use wasi::io::streams::StreamError;
        use wasi::sockets::instance_network;
        use wasi::sockets::network::{
            ErrorCode, IpAddressFamily, IpSocketAddress, Ipv4SocketAddress,
        };
        use wasi::sockets::tcp_create_socket;

        let socket = tcp_create_socket::create_tcp_socket(IpAddressFamily::Ipv4)
            .map_err(|e| format!("create-tcp-socket: {e:?}"))?;
        let network = instance_network::instance_network();
        let remote = IpSocketAddress::Ipv4(Ipv4SocketAddress {
            port: 8099,
            address: (127, 0, 0, 1),
        });
        socket
            .start_connect(&network, remote)
            .map_err(|e| format!("start-connect: {e:?}"))?;
        let (input, output) = loop {
            match socket.finish_connect() {
                Ok(streams) => break streams,
                Err(ErrorCode::WouldBlock) => socket.subscribe().block(),
                Err(e) => return Err(format!("finish-connect: {e:?}")),
            }
        };
        output
            .blocking_write_and_flush(b"ping")
            .map_err(|e| format!("write ping: {e:?}"))?;
        let mut buf = Vec::new();
        while buf.len() < 4 {
            let want = (4 - buf.len()) as u64;
            let chunk = match input.blocking_read(want) {
                Ok(chunk) => chunk,
                Err(StreamError::Closed) => {
                    return Err("input closed before 4-byte pong".into());
                }
                Err(e) => return Err(format!("read pong: {e:?}")),
            };
            if chunk.is_empty() {
                return Err("input produced empty read before 4-byte pong".into());
            }
            buf.extend_from_slice(&chunk);
        }
        let received = String::from_utf8(buf).map_err(|_| "pong is not utf-8".to_string())?;
        drop(input);
        drop(output);
        drop(socket);
        drop(network);
        Ok(received)
    }
}

export!(Fixture);
