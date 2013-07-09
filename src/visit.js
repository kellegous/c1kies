var page = require('webpage').create(),
    system = require('system'),
    fs = require('fs'),
    url = system.args[1],
    dst = system.args[2],
    resources = [],
    timer = -1;

page.viewportSize = {
  width: 1024,
  height: 768
};

var waitForExit = function() {
  if (timer >= 0) {
    clearTimeout(timer);
  }

  timer = setTimeout(function() {
    fs.write(
      [dst, 'cookies.json'].join(fs.separator),
      JSON.stringify(phantom.cookies), 'w');
    fs.write(
      [dst, 'resources.json'].join(fs.separator),
      JSON.stringify(resources), 'w');
    page.render([dst, 'capture.png'].join(fs.separator));
    phantom.exit(0);
  }, 1000);
};

if (!url) {
  console.log('ERROR: no url.');
  phantom.exit(1);
}

page.onResourceReceived = function(resp) {
  resources.push({
    url: resp.url,
    status: resp.status,
    headers: resp.headers,
    contentType: resp.contentType,
    size: resp.bodySize
  });
  waitForExit();
};

page.open(url, function(status) {
  if (status != 'success') {
    console.log('ERROR: page open failed.');
    phantom.exit(1);
  }
  waitForExit();
});