const puppeteer = require('puppeteer');
const sha256 = require('js-sha256').sha256
const tmp = require('tmp')
const fs = require('fs')

if (process.argv.length < 5) {
  console.log("Usage: node " + process.argv[1] + " savedirectory protocol host port")
  process.exit(1)
}
var argv = process.argv.slice(2)
const directory = argv[0]
var protocol = argv[1]
var port = ""
if (argv.length > 3) {
  port = argv[3]
}
if (protocol.startsWith("ssl") || protocol.includes("https")) {
  protocol = "https:"
  if (port == 443) {
    port = ""
  }
} else {
  protocol = "http:"
  if (port == 80) {
    port = ""
  }
}
const hostname = argv[2]
const url = protocol + "//" + hostname + (port ? ":" + port : "");
(async () => {
  const browser = await puppeteer.launch({ignoreHTTPSErrors: true});
  const page = await browser.newPage();
  try {
    await page.goto(url,{waitUntil: "networkidle0"});
  } catch (error) {
    // We will ignore errors here, and just take screenshot
//    await browser.close()
//    process.exit(1)
  }
  try {
    const location = await page.evaluate('document.location')
    if (location.protocol == protocol && location.hostname == hostname && location.port == port) {
      // Write out to a tmp file, hash it, copy it to /data or whatever, return hash
      var path = tmp.fileSync({prefix: 'http-screenshot', postfix: '.png'})
      await page.screenshot({path: path.name, fullPage: true});
      var hash = sha256(fs.readFileSync(path.name))
      var file = directory + '/' + hash + '.png'
      fs.copyFileSync(path.name, file);
      console.log(JSON.stringify({url: location.href, hash: hash, file: file}));
    } else {
      console.log(location)
      browser.close()
      process.exit(1)
    }
    await browser.close();
  } catch (error) {
    browser.close()
    process.exit(2)
  }
})();
