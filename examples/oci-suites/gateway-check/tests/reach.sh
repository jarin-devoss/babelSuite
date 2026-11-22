echo "BABELSUITE_GATEWAY_URL=${BABELSUITE_GATEWAY_URL:-<unset>}"
echo "BABELSUITE_GATEWAY_URLS=${BABELSUITE_GATEWAY_URLS:-<unset>}"
if [ -z "$BABELSUITE_GATEWAY_URL" ]; then
  echo "GATEWAY-CHECK FAIL: no gateway URL in the step environment" >&2
  exit 1
fi
# curl prints 000 itself when nothing answered, so no fallback is appended: one
# that echoed 000 again made the failure read 000000 and pass this check.
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$BABELSUITE_GATEWAY_URL/") || true
echo "gateway responded with HTTP $code"
case "$code" in
  [1-5][0-9][0-9]) ;;
  *) code=000 ;;
esac
if [ "$code" = "000" ]; then
  echo "GATEWAY-CHECK FAIL: gateway unreachable from the step container" >&2
  exit 1
fi
echo "GATEWAY-CHECK PASS: reached the sidecar from a test step"
