// Build with:
// rustc --edition 2024 --target wasm32-wasip2 -C opt-level=z -C strip=symbols
//   -C panic=abort -C lto=fat -o sockets_probe.wasm sockets_probe.rs

use std::net::{Ipv4Addr, SocketAddrV4, TcpListener, TcpStream};

fn main() {
    let _ = TcpStream::connect(SocketAddrV4::new(Ipv4Addr::LOCALHOST, 9));
    if let Ok(listener) = TcpListener::bind(SocketAddrV4::new(Ipv4Addr::LOCALHOST, 0)) {
        let _ = listener.accept();
    }
}
