echo "UI-MOCK gateway=${BABELSUITE_GATEWAY_URL:-<unset>}"
if [ -z "$BABELSUITE_GATEWAY_URL" ]; then
  echo "UI-MOCK FAIL: no gateway URL" >&2
  exit 1
fi
body=$(curl -s --max-time 10 -H "Host: ui-mock.mock.internal" -H "Accept: application/json" "$BABELSUITE_GATEWAY_URL/greeting?scenario=hello")
echo "UI-MOCK body=$body"
case "$body" in
  *"hello from ui-mock"*) echo "UI-MOCK PASS" ;;
  *) echo "UI-MOCK FAIL: unexpected body" >&2; exit 1 ;;
esac
