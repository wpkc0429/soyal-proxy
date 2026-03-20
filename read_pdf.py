import sys
import subprocess
try:
    from pypdf import PdfReader
except ImportError:
    subprocess.check_call([sys.executable, "-m", "pip", "install", "pypdf", "--break-system-packages"])
    from pypdf import PdfReader

pdf_path = sys.argv[1]
text = ''
try:
    reader = PdfReader(pdf_path)
    text = ''.join([p.extract_text() or '' for p in reader.pages])
except Exception as e:
    text = str(e)
with open("protocol.txt", "w", encoding="utf-8") as f:
    f.write(text)
print("Done")
