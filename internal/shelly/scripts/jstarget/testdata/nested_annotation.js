/** @target daemon */
function outer() {
  /** @target daemon */
  function inner() {
    return 1;
  }
  return inner();
}
