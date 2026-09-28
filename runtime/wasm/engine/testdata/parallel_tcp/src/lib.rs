wit_bindgen::generate!({ path: "../tcp/wit", world: "tcp", generate_all });

use wasi::sockets::instance_network;
use wasi::sockets::network::{ErrorCode, IpAddressFamily, IpSocketAddress, Ipv4SocketAddress};
use wasi::sockets::tcp_create_socket;

struct Fixture;

impl Guest for Fixture {
    fn run() -> Result<String, String> {
        let sock1 = tcp_create_socket::create_tcp_socket(IpAddressFamily::Ipv4)
            .map_err(|e| format!("create-tcp-socket 1: {e:?}"))?;
        let sock2 = tcp_create_socket::create_tcp_socket(IpAddressFamily::Ipv4)
            .map_err(|e| format!("create-tcp-socket 2: {e:?}"))?;

        let net = instance_network::instance_network();
        let addr1 = IpSocketAddress::Ipv4(Ipv4SocketAddress {
            port: 8099,
            address: (127, 0, 0, 1),
        });
        let addr2 = IpSocketAddress::Ipv4(Ipv4SocketAddress {
            port: 8100,
            address: (127, 0, 0, 1),
        });

        // BOTH start_connect calls before any finish_connect call.
        sock1.start_connect(&net, addr1)
            .map_err(|e| format!("start-connect 1: {e:?}"))?;
        sock2.start_connect(&net, addr2)
            .map_err(|e| format!("start-connect 2: {e:?}"))?;

        let mut res1 = None;
        let mut res2 = None;

        let p1 = sock1.subscribe();
        let p2 = sock2.subscribe();

        while res1.is_none() || res2.is_none() {
            if res1.is_none() {
                match sock1.finish_connect() {
                    Ok(streams) => res1 = Some(streams),
                    Err(ErrorCode::WouldBlock) => {}
                    Err(e) => return Err(format!("finish-connect 1: {e:?}")),
                }
            }
            if res2.is_none() {
                match sock2.finish_connect() {
                    Ok(streams) => res2 = Some(streams),
                    Err(ErrorCode::WouldBlock) => {}
                    Err(e) => return Err(format!("finish-connect 2: {e:?}")),
                }
            }

            if res1.is_some() && res2.is_some() {
                break;
            }

            if res1.is_none() && res2.is_none() {
                wasi::io::poll::poll(&[&p1, &p2]);
            } else if res1.is_none() {
                wasi::io::poll::poll(&[&p1]);
            } else if res2.is_none() {
                wasi::io::poll::poll(&[&p2]);
            }
        }

        let (in1, out1) = res1.unwrap();
        let (in2, out2) = res2.unwrap();

        drop(in1);
        drop(out1);
        drop(in2);
        drop(out2);
        drop(p1);
        drop(p2);
        drop(sock1);
        drop(sock2);
        drop(net);

        Ok("connected:2".to_string())
    }
}

export!(Fixture);
