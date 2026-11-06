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
- Temperature and Humidity Sensor: JTAREA DHT22 Digital Temperature and Humidity Sensor AM2302 Sensors Module
- Load Cell: ShangHJ 2 Sets Digital Load Cell Weight Sensor + HX711 ADC Module Weighing Sensor

