import json
import os
import sys

# Define paths
BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if "__compiled__" in globals():
    if os.name == "posix":
        BASE_DIR = os.getcwd()
    else:
        BASE_DIR = os.path.dirname(sys.executable)

CONFIG_PATH = os.path.join(BASE_DIR, "config.json")
UPLOAD_PATH = os.path.join(BASE_DIR, "uploads")

# Ensure directories exist
os.makedirs(UPLOAD_PATH, exist_ok=True)

class Config:
    def __init__(self):
        self.data = {}
        self.load()

    def load(self):
        try:
            with open(CONFIG_PATH, "r", encoding="utf-8") as f:
                self.data = json.load(f)
        except Exception:
            # Create default config if not exists
            self.data = {"site_title": "文件中转站"}
            self.save()

        if self.data.get("shutdown"):
            print("Service shutdown requested.")
            os._exit(0)

    def save(self):
        with open(CONFIG_PATH, "w", encoding="utf-8") as f:
            json.dump(self.data, f, ensure_ascii=False, indent=4)

    def get(self, key, default=None):
        return self.data.get(key, default)

config = Config()
