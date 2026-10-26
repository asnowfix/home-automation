function before() {
  return 1;
}

/** @target daemon */
function middle(body) {
  return body;
}

function after() {
  return 2;
}
