wit_bindgen::generate!({ path: "wit", world: "actor", with: { "wasi:io/poll@0.2.8": generate } });
struct GuestActor;
impl Guest for GuestActor {
    fn run() -> Result<(), String> {
        let mailbox: wasi::io::poll::Pollable = wippy::actor::process::subscribe();
        let ready = wasi::io::poll::poll(&[&mailbox]);
        if ready != [0] { return Err("mailbox poll missed queued message".into()); }
        if wippy::actor::process::try_receive()?.is_none() { return Err("poll consumed message".into()); }
        drop(mailbox);
        Ok(())
    }
}
export!(GuestActor);
