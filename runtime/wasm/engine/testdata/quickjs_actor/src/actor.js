// One QuickJS context belongs to this actor for its entire lifetime.
// receive() parks this JavaScript call stack until Wippy delivers a message.
const state = { count: 0n };
for (;;) {
  const message = actor.receive();
  if (message.topic === "stop") break;

  let reply;
  if (message.topic === "increment") {
    state.count += 1n;
    reply = `count:${state.count}`;
  } else {
    reply = `unknown:${message.topic}`;
  }
  if (!actor.sendText(message.from, "quickjs", reply)) {
    throw new Error("reply recipient is unavailable");
  }
}
