mkdir -p reports
cat > reports/ctrf.json <<'JSON'
{"results": {"tool": {"name": "ui"}, "summary": {"tests": 3, "passed": 2, "failed": 1, "skipped": 0, "pending": 0, "other": 0, "start": 1000, "stop": 3000}, "tests": [{"name": "a", "status": "passed", "duration": 1}, {"name": "b", "status": "passed", "duration": 1}, {"name": "c", "status": "failed", "duration": 1}]}}
JSON
echo "UI-CTRF wrote reports/ctrf.json"
