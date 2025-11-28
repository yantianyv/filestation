import os
import uuid
import json
from datetime import datetime, timedelta
from flask import Blueprint, render_template, request, send_from_directory, jsonify, current_app
from werkzeug.utils import secure_filename
from werkzeug.security import generate_password_hash, check_password_hash
from app.config import UPLOAD_PATH
from app.utils import get_client_info

bp = Blueprint('main', __name__)

@bp.context_processor
def inject_now():
    return {"now": datetime.now}

@bp.route("/")
def index():
    from app.utils import get_temp_files
    return render_template("index.html",
                         temp_files=get_temp_files(),
                         site_title="文件中转站")

@bp.route("/upload", methods=["GET", "POST"])
def upload_file():
    if request.method == "POST":
        if "file" not in request.files:
            return jsonify({"success": False, "message": "No file selected"}), 400

        file = request.files["file"]
        if file.filename == "":
            return jsonify({"success": False, "message": "No file selected"}), 400

        if file:
            try:
                filename = secure_filename(file.filename)
                unique_filename = f"{uuid.uuid4().hex[:8]}_{filename}"
                filepath = os.path.join(UPLOAD_PATH, unique_filename)
                temp_filepath = filepath + ".part"

                # Use Flask's save method which is optimized (uses shutil.copyfileobj)
                file.save(temp_filepath)

                # Atomic move after write is complete
                os.rename(temp_filepath, filepath)

                description = request.form.get("description", "").strip() or "上传者没有提供描述信息"
                password = request.form.get("password", "").strip()
                expiration_hours = int(request.form.get("expiration", 24))

                upload_time = datetime.now()
                expiration_time = upload_time + timedelta(hours=expiration_hours)

                desc_data = {
                    "description": description,
                    "uploader": get_client_info(),
                    "upload_time": upload_time.isoformat(),
                    "expiration_time": expiration_time.isoformat(),
                    "original_filename": file.filename
                }

                if password:
                    desc_data["password_hash"] = generate_password_hash(password)

                with open(os.path.join(UPLOAD_PATH, f".{unique_filename}.json"), "w", encoding="utf-8") as f:
                    json.dump(desc_data, f, ensure_ascii=False, indent=2)

                return jsonify({"success": True, "message": "File uploaded successfully!"})
            except Exception as e:
                current_app.logger.error(f"Error uploading file: {e}")
                if os.path.exists(filepath):
                    os.remove(filepath)
                return jsonify({"success": False, "message": "Upload failed"}), 500

    return render_template("upload.html", site_title="文件中转站")

@bp.route("/download/<path:filename>", methods=["GET", "POST"])
def download_file(filename):
    try:
        filepath = os.path.normpath(filename)
        full_path = os.path.join(UPLOAD_PATH, filepath)

        if os.path.exists(full_path):
            desc_file = os.path.join(UPLOAD_PATH, f".{filepath}.json")
            original_filename = filepath

            password_hash = None

            if os.path.exists(desc_file):
                try:
                    with open(desc_file, "r", encoding="utf-8") as f:
                        data = json.load(f)
                        original_filename = data.get("original_filename", filepath)
                        password_hash = data.get("password_hash")
                except:
                    pass

            # Check password protection
            if password_hash:
                if request.method == "POST":
                    password = request.form.get("password", "")
                    if check_password_hash(password_hash, password):
                        # Password correct, proceed to download
                        pass
                    else:
                        return render_template("password.html",
                                            filename=original_filename,
                                            error=True,
                                            site_title="文件中转站")
                else:
                    return render_template("password.html",
                                        filename=original_filename,
                                        error=False,
                                        site_title="文件中转站")

            return send_from_directory(
                directory=UPLOAD_PATH,
                path=filepath,
                as_attachment=True,
                download_name=original_filename
            )
        else:
            return "File not found", 404
    except Exception as e:
        current_app.logger.error(f"Error downloading file {filename}: {e}")
        return "Error downloading file", 500
