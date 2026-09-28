wit_bindgen::generate!({ path: "../mqtt/wit", world: "mqtt", generate_all });

use wasi::io::poll::{self, Pollable};
use wasi::io::streams::{InputStream, OutputStream, StreamError};
use wasi::sockets::instance_network;
use wasi::sockets::network::{ErrorCode, IpAddressFamily, IpSocketAddress, Ipv4SocketAddress};
use wasi::sockets::tcp::TcpSocket;
use wasi::sockets::tcp_create_socket;

const LISTEN_PORT: u16 = 8099;
const EXPECTED_CLIENTS: u32 = 8;
const FRAME_SIZE: usize = 64;
const MAX_SLOTS: usize = 8;
const MAX_POLL: usize = MAX_SLOTS + 1;

struct Fixture;

impl Guest for Fixture {
    fn run() -> Result<String, String> {
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

        let accept_sub = socket.subscribe();
        let mut slots: [Option<Conn>; MAX_SLOTS] = [None, None, None, None, None, None, None, None];
        let mut accepted: u32 = 0;
        let mut completed: u32 = 0;
        let mut frames: u64 = 0;

        while completed < EXPECTED_CLIENTS {
            let mut progressed = false;

            if accepted < EXPECTED_CLIENTS {
                if let Some(idx) = slots.iter().position(Option::is_none) {
                    match socket.accept() {
                        Ok((client, input, output)) => {
                            let in_sub = input.subscribe();
                            let out_sub = output.subscribe();
                            slots[idx] = Some(Conn {
                                in_sub,
                                out_sub,
                                input,
                                output,
                                socket: client,
                                frame: [0; FRAME_SIZE],
                                read_len: 0,
                                write_off: 0,
                                flushing: false,
                                frames: 0,
                            });
                            accepted += 1;
                            progressed = true;
                        }
                        Err(ErrorCode::WouldBlock) | Err(ErrorCode::ConnectionAborted) => {}
                        Err(e) => return Err(format!("accept: {e:?}")),
                    }
                }
            }

            for slot in &mut slots {
                let outcome = match slot.as_mut() {
                    None => continue,
                    Some(conn) => conn.step()?,
                };
                match outcome {
                    Step::None => {}
                    Step::Progress => progressed = true,
                    Step::Done => {
                        frames += slot.as_ref().unwrap().frames;
                        completed += 1;
                        close_conn(slot.take().unwrap());
                        progressed = true;
                    }
                }
            }

            if completed == EXPECTED_CLIENTS {
                break;
            }
            if progressed {
                continue;
            }
            wait_ready(&accept_sub, accepted < EXPECTED_CLIENTS, &slots)?;
        }

        for slot in &mut slots {
            if let Some(conn) = slot.take() {
                close_conn(conn);
            }
        }
        drop(accept_sub);
        drop(socket);
        drop(network);
        Ok(format!("frames:{frames}"))
    }
}

enum Step {
    None,
    Progress,
    Done,
}

struct Conn {
    in_sub: Pollable,
    out_sub: Pollable,
    input: InputStream,
    output: OutputStream,
    socket: TcpSocket,
    frame: [u8; FRAME_SIZE],
    read_len: usize,
    write_off: usize,
    flushing: bool,
    frames: u64,
}

impl Conn {
    fn awaiting_output(&self) -> bool {
        self.flushing || self.read_len == FRAME_SIZE
    }

    fn step(&mut self) -> Result<Step, String> {
        if self.flushing {
            return self.step_flush();
        }
        if self.read_len == FRAME_SIZE {
            return self.step_write();
        }
        self.step_read()
    }

    fn step_read(&mut self) -> Result<Step, String> {
        let want = (FRAME_SIZE - self.read_len) as u64;
        match self.input.read(want) {
            Ok(chunk) if chunk.is_empty() => Ok(Step::None),
            Ok(chunk) => {
                if chunk.len() > FRAME_SIZE - self.read_len {
                    return Err("read exceeded 64-byte frame".into());
                }
                let n = chunk.len();
                self.frame[self.read_len..self.read_len + n].copy_from_slice(&chunk);
                self.read_len += n;
                Ok(Step::Progress)
            }
            Err(StreamError::Closed) => {
                if self.read_len == 0 {
                    Ok(Step::Done)
                } else {
                    Err(format!(
                        "partial EOF: input closed at {} of 64-byte frame",
                        self.read_len
                    ))
                }
            }
            Err(e) => Err(format!("read: {e:?}")),
        }
    }

    fn step_write(&mut self) -> Result<Step, String> {
        match self.output.check_write() {
            Ok(0) => Ok(Step::None),
            Ok(permit) => {
                let remaining = FRAME_SIZE - self.write_off;
                let n = remaining.min(permit as usize);
                self.output
                    .write(&self.frame[self.write_off..self.write_off + n])
                    .map_err(|e| format!("write: {e:?}"))?;
                self.write_off += n;
                if self.write_off == FRAME_SIZE {
                    self.output.flush().map_err(|e| format!("flush: {e:?}"))?;
                    self.flushing = true;
                }
                Ok(Step::Progress)
            }
            Err(StreamError::Closed) => Err("output closed during write".into()),
            Err(e) => Err(format!("check-write: {e:?}")),
        }
    }

    fn step_flush(&mut self) -> Result<Step, String> {
        match self.output.check_write() {
            Ok(0) => Ok(Step::None),
            Ok(_) => {
                self.flushing = false;
                self.read_len = 0;
                self.write_off = 0;
                self.frames += 1;
                Ok(Step::Progress)
            }
            Err(StreamError::Closed) => Err("output closed during flush".into()),
            Err(e) => Err(format!("check-write: {e:?}")),
        }
    }
}

fn wait_ready(
    accept: &Pollable,
    accepting: bool,
    slots: &[Option<Conn>; MAX_SLOTS],
) -> Result<(), String> {
    // Borrowed handles fit on the guest stack. No Vec allocation for the input handle list.
    let mut list = [accept; MAX_POLL];
    let mut count = 0;
    if accepting {
        list[count] = accept;
        count += 1;
    }
    for conn in slots.iter().flatten() {
        list[count] = if conn.awaiting_output() {
            &conn.out_sub
        } else {
            &conn.in_sub
        };
        count += 1;
    }
    if count == 0 {
        return Err("poll list empty".into());
    }
    poll::poll(&list[..count]);
    Ok(())
}

fn close_conn(conn: Conn) {
    let Conn {
        in_sub,
        out_sub,
        input,
        output,
        socket,
        ..
    } = conn;
    drop(in_sub);
    drop(out_sub);
    drop(input);
    drop(output);
    drop(socket);
}

export!(Fixture);
