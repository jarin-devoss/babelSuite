i=1
while [ "$i" -le 45 ]; do
  echo "TICK $i"
  i=$((i + 1))
  sleep 2
done
echo "TICK done"
