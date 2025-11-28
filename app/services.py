import time
import threading
from app.utils import cleanup_temp_files

def cleanup_task():
    while True:
        try:
            cleanup_temp_files()
        except Exception as e:
            print(f"Error cleaning up temp files: {e}")
        time.sleep(3600)  # Clean up every hour

def start_background_tasks():
    cleanup_thread = threading.Thread(
        target=cleanup_task,
        daemon=True
    )
    cleanup_thread.start()
