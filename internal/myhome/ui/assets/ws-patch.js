(function () {
  try {
    function nonEmptySegments(p) {
      if (!p) return [];
      var out = [];
      var parts = p.split('/');
      for (var i = 0; i < parts.length; i++) if (parts[i]) out.push(parts[i]);
      return out;
    }

    var segs = nonEmptySegments(window.location.pathname);
    var T = '';
    if (segs.length >= 2 && segs[0] === 'devices') {
      T = segs[1];
    }
    if (!T) return; // nothing to patch on index page

    var P = '/devices/' + T;
    var O = window.WebSocket;
    if (!O) return;

    function sameHost(a, b) {
      return a === b || a.replace(/\.$/, '') === b.replace(/\.$/, '');
    }

    function wrap(u, p) {
      try {
        var U = new URL(u, window.location.href);
        var isSameOrigin = sameHost(U.host, window.location.host);
        var isTargetToken = sameHost(U.hostname, T) || sameHost(U.hostname, T + '.local');
        if (isSameOrigin || isTargetToken) {
          // force same-origin if targeting the token host
          if (isTargetToken && !isSameOrigin) {
            U.protocol = window.location.protocol;
            U.host = window.location.host;
          }
          if (U.pathname.indexOf(P + '/') !== 0) {
            U.pathname = P + (U.pathname[0] == '/' ? '' : '/') + U.pathname;
          }
          u = U.toString();
        }
      } catch (e) {}
      return new O(u, p);
    }

    wrap.prototype = O.prototype;
    wrap.CONNECTING = O.CONNECTING;
    wrap.OPEN = O.OPEN;
    wrap.CLOSING = O.CLOSING;
    wrap.CLOSED = O.CLOSED;
    window.WebSocket = wrap;
  } catch (e) {}
})();
