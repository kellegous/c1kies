var page = require('webpage').create(),
    system = require('system'),
    fs = require('fs'),
    url = system.args[1],
    cookies = system.args[2],
    timer = -1;

var waitForExit = function() {
  if (timer >= 0) {
    clearTimeout(timer);
  }

  timer = setTimeout(function() {
    // write the cookies
    fs.write(cookies, JSON.stringify(phantom.cookies), 'w');
    phantom.exit(0);
  }, 1000);
};

if (!url) {
  console.log('ERROR: no url.');
  phantom.exit(1);
}

page.onResourceReceived = function(resp) {
  console.log(resp.url);
  waitForExit();
};

page.open(url, function(status) {
  if (status != 'success') {
    console.log('ERROR: page open failed.');
    phantom.exit(1);
  }
  waitForExit();
});