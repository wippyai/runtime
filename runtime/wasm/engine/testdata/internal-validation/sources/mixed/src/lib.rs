wit_bindgen::generate!({ path: "wit", world: "tcp", generate_all });
struct Fixture;
impl Guest for Fixture {
    fn run() -> Result<String, String> {
        use wasi::io::poll;
        use wasi::sockets::instance_network;
        use wasi::sockets::network::{ErrorCode, IpAddressFamily, IpSocketAddress, Ipv4SocketAddress};
        use wasi::sockets::tcp_create_socket;
        use wippy::actor::process;
        let socket = tcp_create_socket::create_tcp_socket(IpAddressFamily::Ipv4).map_err(|e| format!("create: {e:?}"))?;
        let network = instance_network::instance_network();
        socket.start_bind(&network, IpSocketAddress::Ipv4(Ipv4SocketAddress { port: 38135, address: (127, 0, 0, 1) })).map_err(|e| format!("start-bind: {e:?}"))?;
        loop { match socket.finish_bind() { Ok(()) => break, Err(ErrorCode::WouldBlock) => socket.subscribe().block(), Err(e) => return Err(format!("finish-bind: {e:?}")) } }
        socket.start_listen().map_err(|e| format!("start-listen: {e:?}"))?;
        loop { match socket.finish_listen() { Ok(()) => break, Err(ErrorCode::WouldBlock) => socket.subscribe().block(), Err(e) => return Err(format!("finish-listen: {e:?}")) } }
        let mailbox: poll::Pollable = process::subscribe();
        let socket_ready = socket.subscribe();
        loop { for index in poll::poll(&[&mailbox, &socket_ready]) { match index {
            0 => while let Some(message) = process::try_receive()? { if message.topic == "stop" { drop(socket_ready); drop(mailbox); drop(socket); drop(network); return Ok("mailbox".into()) } },
            1 => match socket.accept() { Ok((client, input, output)) => { drop(input); drop(output); drop(client); drop(socket_ready); drop(mailbox); drop(socket); drop(network); return Ok("socket".into()) }, Err(ErrorCode::WouldBlock) => {}, Err(e) => return Err(format!("accept: {e:?}")) },
            _ => return Err("unexpected poll index".into()),
        } } }
    }
}
export!(Fixture);
