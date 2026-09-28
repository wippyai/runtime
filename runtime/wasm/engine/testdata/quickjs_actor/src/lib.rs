wit_bindgen::generate!({ path: "wit", world: "actor", with: { "wasi:io/poll@0.2.8": generate } });

use rquickjs::{function::Func, CaughtError, Context, Ctx, Exception, Object, Runtime};
use wippy::actor::process;

fn receive<'js>(ctx: Ctx<'js>) -> rquickjs::Result<Object<'js>> {
    let message = process::receive().map_err(|err| Exception::throw_message(&ctx, &err))?;
    let result = Object::new(ctx.clone())?;
    result.set("from", message.from)?;
    result.set("topic", message.topic)?;
    let payloads = rquickjs::Array::new(ctx.clone())?;
    for (index, payload) in message.payloads.into_iter().enumerate() {
        let item = Object::new(ctx.clone())?;
        item.set("format", payload.format)?;
        item.set("data", payload.data)?;
        payloads.set(index, item)?;
    }
    result.set("payloads", payloads)?;
    Ok(result)
}

fn send_text(ctx: Ctx<'_>, target: String, topic: String, text: String) -> rquickjs::Result<bool> {
    process::send(
        &target,
        &topic,
        &[process::Payload {
            format: "text".into(),
            data: text.into_bytes(),
        }],
    )
    .map_err(|err| Exception::throw_message(&ctx, &err))
}

struct Actor;
impl Guest for Actor {
    fn run() -> Result<(), String> {
        let runtime = Runtime::new().map_err(|err| err.to_string())?;
        runtime.set_memory_limit(8 << 20);
        let context = Context::full(&runtime).map_err(|err| err.to_string())?;
        context.with(|ctx| {
            let result = (|| -> rquickjs::Result<()> {
                let actor = Object::new(ctx.clone())?;
                actor.set("receive", Func::from(receive))?;
                actor.set("sendText", Func::from(send_text))?;
                actor.set("self", Func::from(process::self_))?;
                ctx.globals().set("actor", actor)?;
                ctx.eval::<(), _>(include_str!("actor.js"))
            })();
            result.map_err(|err| CaughtError::from_error(&ctx, err).to_string())
        })
    }
}
export!(Actor);
