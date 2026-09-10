wit_bindgen::generate!({ path: "wit", world: "mqtt", generate_all });

use wasi::io::streams::{InputStream, OutputStream, StreamError};
use wasi::sockets::network::ErrorCode;
use wasi::sockets::tcp::TcpSocket;

const LISTEN_PORT: u16 = 1883;
const EXPECTED_CLIENTS: u32 = 2;
const MAX_REMAINING: usize = 4096;
const MAX_PACKET: usize = 1 + 4 + MAX_REMAINING;

const CONNECT: u8 = 0x10;
const PINGREQ: u8 = 0xC0;
const DISCONNECT: u8 = 0xE0;
const CONNACK: [u8; 4] = [0x20, 0x02, 0x00, 0x00];
const PINGRESP: [u8; 2] = [0xD0, 0x00];
const CONNECT_FLAGS: u8 = 0x02;
const PROTOCOL_LEVEL_311: u8 = 4;

struct Fixture;

impl Guest for Fixture {
    fn run() -> Result<String, String> {
        use wasi::sockets::instance_network;
        use wasi::sockets::network::{IpAddressFamily, IpSocketAddress, Ipv4SocketAddress};
        use wasi::sockets::tcp_create_socket;

        let socket = tcp_create_socket::create_tcp_socket(IpAddressFamily::Ipv4)
            .map_err(|e| format!("create-tcp-socket: {e:?}"))?;
        let network = instance_network::instance_network();
        let local = IpSocketAddress::Ipv4(Ipv4SocketAddress {
            port: LISTEN_PORT,
            address: (127, 0, 0, 1),
        });
        socket
            .start_bind(&network, local)
            .map_err(|e| format!("start-bind: {e:?}"))?;
        loop {
            match socket.finish_bind() {
                Ok(()) => break,
                Err(ErrorCode::WouldBlock) => socket.subscribe().block(),
                Err(e) => return Err(format!("finish-bind: {e:?}")),
            }
        }
        socket
            .start_listen()
            .map_err(|e| format!("start-listen: {e:?}"))?;
        loop {
            match socket.finish_listen() {
                Ok(()) => break,
                Err(ErrorCode::WouldBlock) => socket.subscribe().block(),
                Err(e) => return Err(format!("finish-listen: {e:?}")),
            }
        }

        let mut served: u32 = 0;
        while served < EXPECTED_CLIENTS {
            let (client, input, output) = loop {
                match socket.accept() {
                    Ok(accepted) => break accepted,
                    Err(ErrorCode::WouldBlock) => socket.subscribe().block(),
                    Err(e) => return Err(format!("accept: {e:?}")),
                }
            };
            serve_client(client, input, output)?;
            served += 1;
        }
        drop(socket);
        drop(network);
        Ok(format!("served:{served}"))
    }
}

fn serve_client(client: TcpSocket, input: InputStream, output: OutputStream) -> Result<(), String> {
    let mut conn = Conn {
        input,
        output,
        buf: Vec::new(),
    };
    let (first, payload) = conn.read_packet()?;
    if first != CONNECT {
        return Err(format!("expected CONNECT, got {first:#04x}"));
    }
    parse_connect(&payload)?;
    conn.write_all(&CONNACK)?;

    let (first, payload) = conn.read_packet()?;
    if first != PINGREQ || !payload.is_empty() {
        return Err(format!(
            "expected PINGREQ, got {first:#04x} remaining {}",
            payload.len()
        ));
    }
    conn.write_all(&PINGRESP)?;

    let (first, payload) = conn.read_packet()?;
    if first != DISCONNECT || !payload.is_empty() {
        return Err(format!(
            "expected DISCONNECT, got {first:#04x} remaining {}",
            payload.len()
        ));
    }
    drop(conn.input);
    drop(conn.output);
    drop(client);
    Ok(())
}

struct Conn {
    input: InputStream,
    output: OutputStream,
    buf: Vec<u8>,
}

impl Conn {
    fn write_all(&self, data: &[u8]) -> Result<(), String> {
        self.output
            .blocking_write_and_flush(data)
            .map_err(|e| format!("write: {e:?}"))
    }

    fn read_packet(&mut self) -> Result<(u8, Vec<u8>), String> {
        loop {
            match remaining_length(&self.buf)? {
                None => self.fill_more()?,
                Some((len, nlen)) => {
                    let total = 1 + nlen + len;
                    if total > MAX_PACKET {
                        return Err("packet exceeds 4096 remaining-length bound".into());
                    }
                    while self.buf.len() < total {
                        self.fill_more()?;
                    }
                    let packet: Vec<u8> = self.buf.drain(..total).collect();
                    return Ok((packet[0], packet[1 + nlen..].to_vec()));
                }
            }
        }
    }

    fn fill_more(&mut self) -> Result<(), String> {
        if self.buf.len() >= MAX_PACKET {
            return Err("packet exceeds 4096 remaining-length bound".into());
        }
        let want = (MAX_PACKET - self.buf.len()) as u64;
        let chunk = match self.input.blocking_read(want) {
            Ok(chunk) => chunk,
            Err(StreamError::Closed) => {
                return Err("input closed before complete packet".into());
            }
            Err(e) => return Err(format!("read: {e:?}")),
        };
        if chunk.is_empty() {
            return Err("empty read before complete packet".into());
        }
        if self.buf.len() + chunk.len() > MAX_PACKET {
            return Err("packet exceeds 4096 remaining-length bound".into());
        }
        self.buf.extend_from_slice(&chunk);
        Ok(())
    }
}

fn remaining_length(buf: &[u8]) -> Result<Option<(usize, usize)>, String> {
    if buf.is_empty() {
        return Ok(None);
    }
    let mut value: usize = 0;
    let mut multiplier: usize = 1;
    for i in 0..4 {
        let idx = 1 + i;
        if idx >= buf.len() {
            return Ok(None);
        }
        let encoded = buf[idx] as usize;
        value += (encoded & 127) * multiplier;
        if value > MAX_REMAINING {
            return Err("remaining length exceeds 4096".into());
        }
        if encoded & 128 == 0 {
            return Ok(Some((value, i + 1)));
        }
        multiplier *= 128;
    }
    Err("remaining length encoding exceeds 4 bytes".into())
}

fn parse_connect(payload: &[u8]) -> Result<(), String> {
    let (name, rest) = mqtt_string(payload)?;
    if name != b"MQTT" {
        return Err("protocol name is not MQTT".into());
    }
    if rest.is_empty() {
        return Err("truncated protocol level".into());
    }
    if rest[0] != PROTOCOL_LEVEL_311 {
        return Err(format!("protocol level {}, want 4", rest[0]));
    }
    let rest = &rest[1..];
    if rest.is_empty() {
        return Err("truncated connect flags".into());
    }
    if rest[0] != CONNECT_FLAGS {
        return Err(format!(
            "connect flags {:#04x}: require clean-session, no will, no credentials",
            rest[0]
        ));
    }
    let rest = &rest[1..];
    if rest.len() < 2 {
        return Err("truncated keep-alive".into());
    }
    let rest = &rest[2..];
    let (_client_id, rest) = mqtt_string(rest)?;
    if !rest.is_empty() {
        return Err("trailing connect payload".into());
    }
    Ok(())
}

fn mqtt_string(data: &[u8]) -> Result<(&[u8], &[u8]), String> {
    if data.len() < 2 {
        return Err("truncated mqtt string".into());
    }
    let len = u16::from_be_bytes([data[0], data[1]]) as usize;
    let data = &data[2..];
    if data.len() < len {
        return Err("truncated mqtt string payload".into());
    }
    let (s, rest) = data.split_at(len);
    let text = std::str::from_utf8(s).map_err(|_| "mqtt string is not utf-8".to_string())?;
    if text.contains('\0') {
        return Err("mqtt string contains null".into());
    }
    Ok((s, rest))
}

export!(Fixture);
