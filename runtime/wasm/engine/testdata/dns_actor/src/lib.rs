wit_bindgen::generate!({ path: "wit", world: "dns-actor", generate_all });

use wasi::io::poll::{self, Pollable};
use wasi::sockets::instance_network;
use wasi::sockets::ip_name_lookup::{self, ResolveAddressStream};
use wasi::sockets::network::{ErrorCode, IpAddress};

const LITERAL: &str = "::ffff:192.0.2.9";
const INVALID: &str = "a..b";
const IDNA_HOST: &str = "bücher.example";
const SECOND_HOST: &str = "second.example";
const CANCEL_HOST: &str = "cancel.example";
const TIMEOUT_HOST: &str = "timeout.example";

const LITERAL_V4: IpAddress = IpAddress::Ipv4((192, 0, 2, 9));
const FIRST_ADDRS: [IpAddress; 3] = [
    IpAddress::Ipv4((192, 0, 2, 1)),
    IpAddress::Ipv6((0x2001, 0xdb8, 0, 0, 0, 0, 0, 1)),
    IpAddress::Ipv4((198, 51, 100, 7)),
];
const SECOND_ADDR: IpAddress = IpAddress::Ipv4((203, 0, 113, 8));

struct Fixture;

impl Guest for Fixture {
    fn run() -> Result<String, String> {
        let network = instance_network::instance_network();

        let literal = ip_name_lookup::resolve_addresses(&network, LITERAL)
            .map_err(|e| format!("resolve-addresses {LITERAL}: {e:?}"))?;
        expect_immediate(&literal, Some(LITERAL_V4), LITERAL)?;
        expect_immediate(&literal, None, LITERAL)?;
        drop(literal);

        match ip_name_lookup::resolve_addresses(&network, INVALID) {
            Err(ErrorCode::InvalidArgument) => {}
            Ok(stream) => {
                drop(stream);
                return Err(format!("{INVALID} produced a stream"));
            }
            Err(e) => return Err(format!("resolve-addresses {INVALID}: {e:?}")),
        }

        let first = ip_name_lookup::resolve_addresses(&network, IDNA_HOST)
            .map_err(|e| format!("resolve-addresses {IDNA_HOST}: {e:?}"))?;
        let second = ip_name_lookup::resolve_addresses(&network, SECOND_HOST)
            .map_err(|e| format!("resolve-addresses {SECOND_HOST}: {e:?}"))?;

        expect_would_block(&first, IDNA_HOST)?;
        expect_would_block(&second, SECOND_HOST)?;

        let first_sub = first.subscribe();
        let second_sub = second.subscribe();
        poll::poll(&[&first_sub, &second_sub]);

        let mut n = 0u32;
        n += collect_exact(&first, &first_sub, &FIRST_ADDRS, IDNA_HOST)?;
        n += collect_exact(&second, &second_sub, &[SECOND_ADDR], SECOND_HOST)?;

        drop(first_sub);
        drop(second_sub);
        drop(first);
        drop(second);
        drop(network);
        Ok(format!("dns:{n}"))
    }

    fn wait() -> Result<String, String> {
        let network = instance_network::instance_network();
        let stream = ip_name_lookup::resolve_addresses(&network, CANCEL_HOST)
            .map_err(|e| format!("resolve-addresses {CANCEL_HOST}: {e:?}"))?;
        let ready = stream.subscribe();
        loop {
            match stream.resolve_next_address() {
                Err(ErrorCode::WouldBlock) => {
                    poll::poll(&[&ready]);
                }
                Ok(None) => {
                    drop(ready);
                    drop(stream);
                    drop(network);
                    return Err(format!("{CANCEL_HOST} completed with none"));
                }
                Ok(Some(addr)) => {
                    drop(ready);
                    drop(stream);
                    drop(network);
                    return Err(format!("{CANCEL_HOST} completed with {addr:?}"));
                }
                Err(e) => {
                    drop(ready);
                    drop(stream);
                    drop(network);
                    return Err(format!("{CANCEL_HOST} error {e:?}"));
                }
            }
        }
    }

    fn timeout() -> Result<String, String> {
        let network = instance_network::instance_network();
        let stream = ip_name_lookup::resolve_addresses(&network, TIMEOUT_HOST)
            .map_err(|e| format!("resolve-addresses {TIMEOUT_HOST}: {e:?}"))?;
        let ready = stream.subscribe();
        loop {
            match stream.resolve_next_address() {
                Err(ErrorCode::Timeout) => {
                    drop(ready);
                    drop(stream);
                    drop(network);
                    return Ok("timeout".into());
                }
                Err(ErrorCode::WouldBlock) => {
                    poll::poll(&[&ready]);
                }
                Ok(None) => {
                    drop(ready);
                    drop(stream);
                    drop(network);
                    return Err(format!("{TIMEOUT_HOST} completed with none"));
                }
                Ok(Some(addr)) => {
                    drop(ready);
                    drop(stream);
                    drop(network);
                    return Err(format!("{TIMEOUT_HOST} completed with {addr:?}"));
                }
                Err(e) => {
                    drop(ready);
                    drop(stream);
                    drop(network);
                    return Err(format!("{TIMEOUT_HOST} error {e:?}"));
                }
            }
        }
    }
}

fn expect_immediate(
    stream: &ResolveAddressStream,
    want: Option<IpAddress>,
    name: &str,
) -> Result<(), String> {
    match stream.resolve_next_address() {
        Ok(got) if ip_eq_opt(got.as_ref(), want.as_ref()) => Ok(()),
        Ok(got) => Err(format!("{name}: expected {want:?}, got {got:?}")),
        Err(e) => Err(format!("{name} resolve-next-address: {e:?}")),
    }
}

fn expect_would_block(stream: &ResolveAddressStream, name: &str) -> Result<(), String> {
    match stream.resolve_next_address() {
        Err(ErrorCode::WouldBlock) => Ok(()),
        Ok(got) => Err(format!("{name}: expected would-block, got {got:?}")),
        Err(e) => Err(format!("{name}: expected would-block, got {e:?}")),
    }
}

fn collect_exact(
    stream: &ResolveAddressStream,
    ready: &Pollable,
    expected: &[IpAddress],
    name: &str,
) -> Result<u32, String> {
    let mut n = 0u32;
    for want in expected {
        match next_address(stream, ready)? {
            Some(got) if ip_eq(&got, want) => n += 1,
            Some(got) => return Err(format!("{name}: expected {want:?}, got {got:?}")),
            None => return Err(format!("{name}: unexpected none, want {want:?}")),
        }
    }
    match next_address(stream, ready)? {
        None => Ok(n),
        Some(got) => Err(format!("{name}: unexpected extra address {got:?}")),
    }
}

fn next_address(
    stream: &ResolveAddressStream,
    ready: &Pollable,
) -> Result<Option<IpAddress>, String> {
    loop {
        match stream.resolve_next_address() {
            Ok(addr) => return Ok(addr),
            Err(ErrorCode::WouldBlock) => {
                poll::poll(&[ready]);
            }
            Err(e) => return Err(format!("resolve-next-address: {e:?}")),
        }
    }
}

fn ip_eq_opt(got: Option<&IpAddress>, want: Option<&IpAddress>) -> bool {
    match (got, want) {
        (None, None) => true,
        (Some(got), Some(want)) => ip_eq(got, want),
        _ => false,
    }
}

fn ip_eq(got: &IpAddress, want: &IpAddress) -> bool {
    match (got, want) {
        (IpAddress::Ipv4(a), IpAddress::Ipv4(b)) => a == b,
        (IpAddress::Ipv6(a), IpAddress::Ipv6(b)) => a == b,
        _ => false,
    }
}

export!(Fixture);
