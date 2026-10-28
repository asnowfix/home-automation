/** @target daemon */
function forecastTransform(body) {
  return body;
}

var config = {
  /** @target device */
  localOnly: true,
  shared: 1
};

/** @target both */
function explicitBoth() {
  return 1;
}
