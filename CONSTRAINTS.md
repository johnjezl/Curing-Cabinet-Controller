# CONSTRAINTS

- OS: Raspberry Pi OS Lite (64-bit), preinstalled on the rig. Fixed: the rig
  cannot be reflashed autonomously.
- Do not modify /boot/firmware (config.txt, cmdline.txt); display, SSH and
  labctl access are preconfigured.
- All changes to the Pi's system state go through a repeatable provisioning
  script in the repo. No ad-hoc changes over SSH.
- Display: Hosyond 5 Inch Touchscreen IPS MIPI DSI Display Compatible with Raspberry Pi 5/4/3, 800x480 Pixel Capacitive Screen Driver-Free Interface
- Use Go as the primary development language for this project.
- Airflow Sensor: SparkFun Air Velocity Sensor Breakout - FS3000-1005 (Qwiic).  More information is here: https://learn.sparkfun.com/tutorials/air-velocity-sensor-breakout---fs3000-hookup-guide/all
- Airflow Control: ARCTIC P12 Pro 120 mm PWM Fan (600-3000 RPM)
- The air flow sensor and control will be mounted in an air flow manifold (design here: https://github.com/johnjezl/Curing-Cabinet-AirFlow-Manifold) that concentrates air drawn from the top of the curing cabinet.
- Temperature and Humidity Sensors (two, of different types, mounted together
  at product height, away from the walls, heater and humidifier output):
  - Adafruit Sensirion SHT41 Temperature & Humidity Sensor (STEMMA QT / Qwiic),
    on the same I2C bus as the FS3000 (SHT41 at 0x44, FS3000 at 0x28).
  - JTAREA DHT22 Digital Temperature and Humidity Sensor AM2302 Sensors Module.
  - With two temperature readings, control on the warmer one and alarm when
  they disagree by more than about 1.5 C for a sustained period.
- Load Cell: ShangHJ 2 Sets Digital Load Cell Weight Sensor + HX711 ADC Module Weighing Sensor
- Remote alerting is by push notification (self-hosted ntfy), not a wired
  sounder. The controller sends a heartbeat to an external monitor (Uptime
  Kuma) so a dead controller is also reported. Notifications queue while the
  network is down and send on reconnect. The alarm contact may stay unwired.
- Cabinet: upright manual-defrost freezer, cold-wall design (evaporator in
  the walls, no shelf coils, no evaporator fan). Cooling and heating are
  passive; the only forced airflow is the top exhaust manifold, with a
  passive low inlet.
- Both temperature/humidity sensors (SHT41 and DHT22) are mounted together
  at product height, away from the walls, heater and humidifier output.
- Bench rig: a picnic cooler stand-in with a Peltier cold plate on the wall
  (no cold-side fan), a 12 V heater, and the real manifold on the lid. It is
  not the cabinet; do not tune control constants to it.


  
