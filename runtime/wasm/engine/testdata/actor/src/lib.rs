wit_bindgen::generate!({ path: "actor.wit", world: "actor" });
struct Actor;
impl Guest for Actor {
    fn run() -> Result<(), String> {
        use wippy::actor::process;
        let mut count: u64 = 0;
        loop {
            let message = process::receive()?;
            if message.topic == "stop" { return Ok(()); }
            if message.topic == "probe" {
                let identity = process::self_();
                if process::try_receive()?.is_some() { return Err("unexpected queued message".into()); }
                process::send(&message.from, "identity", &[process::Payload { format: "text".into(), data: identity.into_bytes() }])?;
                continue;
            }
            if message.topic == "deny" {
                match process::send("{local@actors|forbidden}", "denied", &[]) {
                    Err(error) if error == "denied" => {},
                    _ => return Err("send policy not enforced".into()),
                }
            }
            count += 1;
            let data = count.to_le_bytes().to_vec();
            process::send(&message.from, "count", &[process::Payload { format: "bytes".into(), data }])?;
        }
    }
}
export!(Actor);
