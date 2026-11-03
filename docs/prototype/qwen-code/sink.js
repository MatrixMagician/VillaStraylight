// PROTOTYPE (#373): a local stand-in for the usage-statistics host. Accepts TCP on 443,
// logs the peer and the bytes of the first read (a TLS ClientHello shows the SNI), closes.
const net = require("net");
net.createServer((c) => {
  c.once("data", (d) => {
    const sni = (d.toString("latin1").match(/[a-z0-9.-]+\.aliyuncs\.com/) || ["-"])[0];
    console.log(JSON.stringify({ t: new Date().toISOString(), from: c.remoteAddress, bytes: d.length, sni }));
    c.destroy();
  });
  c.on("error", () => {});
}).listen(443, "0.0.0.0");
