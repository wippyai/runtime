wit_bindgen::generate!({ path: "wit", world: "udp-actor", generate_all });

use wasi::io::poll::{self, Pollable};
use wasi::sockets::instance_network;
use wasi::sockets::network::{
    ErrorCode, IpAddressFamily, IpSocketAddress, Ipv4SocketAddress, Ipv6SocketAddress,
};
use wasi::sockets::udp::{IncomingDatagram, OutgoingDatagram, OutgoingDatagramStream};
use wasi::sockets::udp_create_socket;

const BIND_PORT: u16 = 8100;
const ECHO_COUNT: u32 = 3;
const MAX_FRAME: usize = 64;
const MAX_RECEIVE: u64 = 16;

struct Fixture;

impl Guest for Fixture {
    fn run() -> Result<String, String> {
        let socket = udp_create_socket::create_udp_socket(IpAddressFamily::Ipv4)
            .map_err(|e| format!("create-udp-socket: {e:?}"))?;
        let network = instance_network::instance_network();
        let local = IpSocketAddress::Ipv4(Ipv4SocketAddress {
            port: BIND_PORT,
            address: (127, 0, 0, 1),
        });
        socket
            .start_bind(&network, local)
            .map_err(|e| format!("start-bind: {e:?}"))?;
        loop {
            match socket.finish_bind() {
                Ok(()) => break,
                Err(ErrorCode::WouldBlock) => {
                    let ready = socket.subscribe();
                    poll::poll(&[&ready]);
                    drop(ready);
                }
                Err(e) => return Err(format!("finish-bind: {e:?}")),
            }
        }

        let (incoming, outgoing) = socket
            .stream(None)
            .map_err(|e| format!("stream: {e:?}"))?;
        let in_sub = incoming.subscribe();
        let out_sub = outgoing.subscribe();

        let idle = incoming
            .receive(MAX_RECEIVE)
            .map_err(|e| format!("idle receive: {e:?}"))?;
        let idle_empty = idle.is_empty();

        let mut echoed: u32 = 0;
        let mut got_ack = false;
        handle_batch(&outgoing, &out_sub, idle, &mut echoed, &mut got_ack)?;

        while !got_ack {
            let batch = incoming
                .receive(MAX_RECEIVE)
                .map_err(|e| format!("receive: {e:?}"))?;
            if batch.is_empty() {
                poll::poll(&[&in_sub]);
                continue;
            }
            handle_batch(&outgoing, &out_sub, batch, &mut echoed, &mut got_ack)?;
        }

        if echoed != ECHO_COUNT {
            return Err(format!("expected {ECHO_COUNT} echoed datagrams, got {echoed}"));
        }
        if !idle_empty {
            return Err("idle receive(max-results=16) returned datagrams".into());
        }

        drop(in_sub);
        drop(out_sub);
        drop(incoming);
        drop(outgoing);
        drop(socket);
        drop(network);
        Ok(format!("datagrams:{echoed}"))
    }
}

fn handle_batch(
    outgoing: &OutgoingDatagramStream,
    out_sub: &Pollable,
    batch: Vec<IncomingDatagram>,
    echoed: &mut u32,
    got_ack: &mut bool,
) -> Result<(), String> {
    for datagram in batch {
        if datagram.data.len() > MAX_FRAME {
            return Err(format!(
                "datagram exceeds {MAX_FRAME} bytes: {}",
                datagram.data.len()
            ));
        }
        if *echoed < ECHO_COUNT {
            echo(outgoing, out_sub, datagram)?;
            *echoed += 1;
        } else {
            *got_ack = true;
            break;
        }
    }
    Ok(())
}

fn echo(
    outgoing: &OutgoingDatagramStream,
    out_sub: &Pollable,
    incoming: IncomingDatagram,
) -> Result<(), String> {
    let remote = clone_addr(&incoming.remote_address);
    let data = incoming.data;
    loop {
        let permit = outgoing
            .check_send()
            .map_err(|e| format!("check-send: {e:?}"))?;
        if permit == 0 {
            poll::poll(&[out_sub]);
            continue;
        }
        let sent = outgoing
            .send(&[OutgoingDatagram {
                data: data.clone(),
                remote_address: Some(clone_addr(&remote)),
            }])
            .map_err(|e| format!("send: {e:?}"))?;
        if sent == 0 {
            poll::poll(&[out_sub]);
            continue;
        }
        return Ok(());
    }
}

fn clone_addr(addr: &IpSocketAddress) -> IpSocketAddress {
    match addr {
        IpSocketAddress::Ipv4(v4) => IpSocketAddress::Ipv4(Ipv4SocketAddress {
            port: v4.port,
            address: v4.address,
        }),
        IpSocketAddress::Ipv6(v6) => IpSocketAddress::Ipv6(Ipv6SocketAddress {
            port: v6.port,
            flow_info: v6.flow_info,
            address: v6.address,
            scope_id: v6.scope_id,
        }),
    }
}

export!(Fixture);
