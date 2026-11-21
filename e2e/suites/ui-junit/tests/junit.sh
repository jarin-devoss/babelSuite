mkdir -p reports
cat > reports/junit.xml <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="ui-junit" tests="5" failures="2" errors="0" skipped="0">
    <testcase classname="ui" name="case-1"/>
    <testcase classname="ui" name="case-2"/>
    <testcase classname="ui" name="case-3"/>
    <testcase classname="ui" name="case-4"><failure message="expected 4">boom</failure></testcase>
    <testcase classname="ui" name="case-5"><failure message="expected 5">boom</failure></testcase>
  </testsuite>
</testsuites>
XML
echo "UI-JUNIT wrote reports/junit.xml"
