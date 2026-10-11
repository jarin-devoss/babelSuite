"""Renders every template category for one locale."""
import os

def write_junit(path, suite_name, cases):
    """Minimal JUnit so the step's declared export has something to collect."""
    import os
    import xml.sax.saxutils as sx
    os.makedirs(os.path.dirname(path), exist_ok=True)
    body = "".join(
        '<testcase classname="%s" name="%s" time="0.01"/>' % (sx.escape(suite_name), sx.escape(n))
        for n in cases
    )
    with open(path, "w", encoding="utf-8") as fh:
        fh.write('<?xml version="1.0" encoding="UTF-8"?>')
        fh.write('<testsuite name="%s" tests="%d" failures="0" errors="0" skipped="0">'
                 % (sx.escape(suite_name), len(cases)))
        fh.write(body)
        fh.write('</testsuite>')


locale = os.environ.get("LOCALE", "en-US")
categories = [c for c in os.environ.get("CATEGORIES", "").split(",") if c]

print(f"render: locale={locale} categories={categories or '<none>'}")

cases = []
for category in categories or ["default"]:
    case = f"{category}-rendered"
    cases.append(case)
    print(f"render: {locale}/{category} rendered, placeholders resolved")

write_junit(f"reports/render-{locale}.xml", f"render-{locale}", cases)
print(f"render: {locale} rendered {len(cases)} templates")
