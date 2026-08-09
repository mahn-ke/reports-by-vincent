import os
import sys
import logging
from datetime import datetime, timedelta

from flask import Flask, jsonify

app = Flask(__name__)
logging.basicConfig(level=logging.INFO)
log = logging.getLogger(__name__)

GARMIN_EMAIL = os.environ.get("GARMIN_EMAIL", "")
GARMIN_PASSWORD = os.environ.get("GARMIN_PASSWORD", "")

if not GARMIN_EMAIL or not GARMIN_PASSWORD:
    log.warning("GARMIN_EMAIL or GARMIN_PASSWORD not set — /fetch will return 503")


@app.get("/fetch")
def fetch():
    if not GARMIN_EMAIL or not GARMIN_PASSWORD:
        return jsonify({"error": "GARMIN_EMAIL/GARMIN_PASSWORD not configured"}), 503

    try:
        import garminconnect
        api = garminconnect.Garmin(GARMIN_EMAIL, GARMIN_PASSWORD)
        api.login()
    except Exception as e:
        log.error("Garmin login failed: %s", e)
        return jsonify({"error": str(e)}), 500

    end_date = datetime.now().strftime("%Y-%m-%d")
    start_date = (datetime.now() - timedelta(days=365 * 5)).strftime("%Y-%m-%d")

    try:
        result = api.get_body_composition(start_date, end_date)
    except Exception as e:
        log.error("Garmin fetch failed: %s", e)
        return jsonify({"error": str(e)}), 500

    entries = []
    for item in result.get("dateWeightList", []):
        weight_raw = item.get("weight") or 0
        smm_raw = item.get("skeletalMuscleMass") or 0
        bone_raw = item.get("boneMass") or 0

        # Garmin returns weight/mass in grams when > 1000, convert to kg.
        weight = weight_raw / 1000.0 if weight_raw > 1000 else float(weight_raw)
        smm = smm_raw / 1000.0 if smm_raw > 1000 else float(smm_raw)
        bone = bone_raw / 1000.0 if bone_raw > 1000 else float(bone_raw)

        sampled_at = item.get("sampledAt") or item.get("calendarDate")
        if not sampled_at:
            continue

        entries.append({
            "measured_at": sampled_at,
            "weight": round(weight, 1),
            "bmi": float(item.get("bmi") or 0),
            "body_fat": float(item.get("bodyFat") or 0),
            "skeletal_muscle_mass": round(smm, 1),
            "bone_mass": round(bone, 1),
            "body_water": float(item.get("bodyWater") or 0),
        })

    log.info("Returning %d body measurements", len(entries))
    return jsonify(entries)


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
