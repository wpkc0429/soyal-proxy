import codecs

with codecs.open("protocol.txt", "r", "utf-16le", errors="ignore") as f:
    lines = f.readlines()
    for i, line in enumerate(lines):
        text = line.lower()
        if "lift" in text or "floor" in text or "relay" in text or "output" in text:
            if "control" in text or "command" in text or "set" in text or "open" in text or "trigger" in text:
                print(f"[LINE] {i}: {line.strip()}")
                for j in range(1, 10):
                    if i+j < len(lines):
                        print(lines[i+j].strip())
                print("---")
