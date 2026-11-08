# Wiring

Every pin is a BCM GPIO on the Raspberry Pi 5's 40-pin header (RP1). All signal
lines are 3.3 V. **RP1 pins are not 5 V tolerant.**

**Test status: none of this wiring has been connected.** The rig has only the
display attached. See "Not tested" at the end.

## Pin map

The defaults are in `drivers.DefaultConfig`. A pin change also needs the matching
change in `deploy/files` (overlays and `curing-safe-outputs`).

| Signal | GPIO | Header pin | Reset pull | Direction | Notes |
|---|---|---|---|---|---|
| FS3000 SDA | 2 | 3 | up | I2C1 | Qwiic. `/dev/i2c-1`, address 0x28 |
| FS3000 SCL | 3 | 5 | up | I2C1 | Qwiic |
| DHT22 data | 4 | 7 | up | bidirectional | kernel `dht11` driver owns the pin |
| HX711 DOUT | 5 | 29 | up | input, pull-up | idle high means not ready |
| Fan tach | 6 | 31 | up | input, pull-up | external pull-up below |
| Fan PWM | 12 | 32 | down | RP1 PWM0 channel 0 | 25 kHz, through a buffer below |
| HX711 SCK | 13 | 33 | down | output | |
| Relay cool | 17 | 11 | down | output, active high | |
| Relay humid | 22 | 15 | down | output, active high | |
| Relay dehumid | 23 | 16 | down | output, active high | |
| Relay alarm | 24 | 18 | down | output, active high | energized while healthy; see "Alarm contact" |
| Relay buzzer | 25 | 22 | down | output, active high | silent when the Pi is dead; see "Buzzer" |
| Door switch (optional) | 26 | 37 | down | input, pull-up | off by default: set `DoorPin` to 26 |
| Relay heat | 27 | 13 | down | output, active high | |

Power on the header: 3.3 V on pins 1 and 17; 5 V on pins 2 and 4; ground on pins
6, 9, 14, 20, 25, 30, 34 and 39.

Avoid GPIO 0 and 1 (HAT ID EEPROM), and GPIO 14 and 15 unless the UART stays off.

## Power

- Pi and display: the official 5 V, 5 A supply, or one that meets it. An undersized
  supply causes undervoltage resets, and each reset drops all relays.
- Fan: 12 V from a separate supply rated for the fan's label current. Join its
  ground to the Pi's. Never feed 12 V near the header.
- 3.3 V rail: DHT22, FS3000 and HX711 only. Relay coils and the fan must not draw
  from it.
- A relay module powered from 5 V must keep its input logic at 3.3 V (see Relays).

## Sensors

### DHT22 / AM2302 module
- VCC to 3.3 V. **Not 5 V**: the module's data pull-up goes to its VCC, so 5 V
  would put 5 V on GPIO 4.
- Data to GPIO 4. It needs a 4.7 to 10 kOhm pull-up to 3.3 V. Modules usually carry
  one; measure yours with a meter. The Pi's internal pull-up, about 50 kOhm, is
  not strong enough to rely on.
- Keep the lead short. The 3.3 V supply reduces noise margin compared with 5 V.

### SparkFun FS3000-1005
- Qwiic: 3.3 V, GND, SDA, SCL. The board has its own pull-ups, in parallel with the
  Pi's fixed 1.8 kOhm ones on GPIO 2 and 3; this is acceptable at 100 kHz.
- Keep the I2C run short and away from the fan's PWM wire. The sensor's accuracy
  depends on its mounting in the manifold, which software cannot check.

### HX711 and load cell
- VCC and, if the board has it, VDD to 3.3 V, so DOUT is a 3.3 V signal. **Not 5 V**
  unless a level shifter is fitted.
- DOUT to GPIO 5, SCK to GPIO 13.
- Load cell: E+, E-, A+, A-. Colours vary; check the cell's data. Reversed A+ and
  A- makes weight read backwards, which calibration can absorb but which is better
  fixed at the terminals.
- Keep SCK and DOUT short. Software reads at gain 128 on channel A.

## ARCTIC P12 Pro

Standard 4-pin header: GND, +12 V, tach, PWM.

- **PWM is not driven directly from GPIO 12.** The 4-wire fan convention pulls the
  control pin up inside the fan, commonly to 5 V, so a 3.3 V push-pull output can
  source current into the GPIO's clamp. Use an open-drain buffer:
  - 74LVC1G07 powered from 3.3 V, input from GPIO 12, output to the fan's PWM pin.
    It does not invert: leave `FanInvert` false.
  - Or a logic-level N-MOSFET with gate on GPIO 12, source to ground, and drain to
    the PWM pin. It inverts: set `FanInvert` to true.
- At boot the line is low until the application sets a duty. With the buffer, the
  fan sees duty 0. Whether the P12 Pro stops or runs at minimum there is not known
  here. An unplugged control wire normally means full speed. See the table below.
- **Tach is open collector: pull it up to 3.3 V, never 5 V or 12 V.** Use 10 kOhm
  from the tach wire to 3.3 V, with a 1 kOhm series resistor to GPIO 6. Route it
  away from the PWM wire, since coupling looks like extra pulses. The driver drops
  edges under 3 ms apart and treats more than 4000 RPM as a bad reading.
- Software assumes 2 tach pulses per revolution.

### Fan state by situation

"Register" rows were read from the RP1 with no fan attached. "Expected" is reasoning about the
4-wire convention and the 74LVC1G07, not a measurement.

| Situation | GPIO 12 | Fan should see | Basis |
|---|---|---|---|
| Control running | PWM at commanded duty | that duty | register |
| Control hung, frozen or killed; guard hung or killed | PWM at last duty | last duty | register |
| Kernel panic, before reset | last state | probably last duty | not observable |
| Reset to bring-up (20 to 40 s) | `no`, pull-down | duty 0 | register, at start of bring-up |
| After bring-up, before a command | PWM at duty 0 | duty 0 | register |
| Pi off, fan on its own 12 V | buffer unpowered, output open | full speed from the fan's pull-up | expected |

## Relays

Six independent outputs. **The module must be high-level trigger.**

- Input high energizes the relay; input low or open de-energizes it. That is what
  makes the pull-down reset state safe (`docs/adr/0201-relay-pins-and-polarity.md`).
- **Add a 10 kOhm pull-down from each relay input to ground, at the module.** The Pi's internal
  pull-down is weak (about 50 kOhm), and the module's own input bias can overpower it when the
  line is released. Check it with step 6 of the bring-up checklist.
- **Do not use a low-level trigger module.** It would energize at reset and in every
  crash. The driver has no setting to invert this, on purpose. On a jumper-selectable
  module, set H and confirm it before connecting any load: with the Pi off and the
  module powered, every relay must be off.
- The input must turn on reliably from 3.3 V and draw under about 8 mA per channel.
  Modules that need 5 V logic need a driver stage (a ULN2803 or a transistor per
  channel, with its own pull-down).
- An opto-isolated module with separate coil power (JD-VCC) is preferred.

Rate each relay for its load, including inrush. A compressor needs a motor-rated
contactor or relay. Do not switch it directly from a small module.

### Alarm contact (fail-safe)

The alarm relay on GPIO 24 is **energized while the controller is running and has nothing to report**. It
drops on any alarm, and on control death, a service restart, a guard failure, a reset or power loss
(`docs/adr/0402-fail-safe-annunciation.md`). So the terminal you pick decides what the wiring means:

| Terminal | While healthy | Alarm, dead controller, no power | Use for |
|---|---|---|---|
| COM and **NC** | open | **closed** | An independently powered sounder, light or dialer that signals when its circuit closes |
| COM and **NO** | closed | **open** | A supervised input that signals when its loop opens. Alarm panels often call this a "normally closed zone" |

Do not read that name as the terminal: a normally closed zone goes on the relay's **NO** terminal, since
the relay is held energized.

Either way the sounder or dialer must have **its own power**, with a battery if it has to work through a
mains cut. Wired through NC with power taken from the Pi or from the same switched supply, it dies with
what it is watching.

- Rate this relay for **continuous duty**. It stays energized for the life of a cure.
- A remote dialer needs an entry delay of about a minute. The contact reads as alarm through every boot,
  and for at least 15 s after control starts or restarts.
- It only signals controller failure when wired as above. It does not measure the cabinet, which is why
  safety item 5 below stays.

**Fit the Pi 5's RTC battery.** Without it the clock comes back near zero after a power cut, outage
records stay provisional until NTP answers, and a cabinet with no network never gets a trusted outage
length. Seen on the rig: `rtc_epoch=90` after a cut.

**A kernel panic or hang is the known gap.** The kernel is not running, so the line holds and the contact
still reads healthy until the board resets (`docs/adr/0203-watchdog-chain.md`).

### Buzzer (GPIO 25)

**Required: an independently powered sounder on the alarm contact's normally closed side.** The buzzer
relay drops with every other output whenever the application fails, hangs, restarts or loses power, so
the local buzzer is silent in exactly the failures that most need a sound.

The local buzzer is driven on demand through its relay. **It cannot sound when the Pi is dead, or when
power is gone, and no software can change that.** It tells someone standing nearby about alarms that
control knows about, including a blank panel. For a dead controller use the alarm contact above with an
independently powered sounder.

### Door switch (optional)
A normally closed contact between GPIO 26 and ground. Door shut pulls low; door open
or a cut wire reads high, so a broken wire reads as door open.

## Required hardware safety items

Software cannot provide these. The cabinet must not run unattended without them.

1. **Independent thermal cutout on the heater**, wired in series with it and with no
   path through the controller: a thermal fuse or a manual-reset thermostat set above
   the highest legitimate cabinet temperature. A welded relay contact, a wrong sensor
   or a software fault cannot be caught any other way.
2. **Snubbing on inductive loads.** Fit an RC snubber or MOV across the contacts or
   load for the compressor, solenoids and any motor. Relay coils driven by a bare
   transistor need a flyback diode (modules usually have one; check yours).
3. **Mains protection.** Over-current protection rated for each load, an RCD or GFCI,
   earthed metal parts, a proper enclosure with mains and low-voltage kept apart, and
   wiring by someone qualified under local rules.
4. **3.3 V on every signal line.** Check DHT data, HX711 DOUT and tach with a meter
   before connecting them.
5. **A separate temperature check.** The controller is not a safety device. An
   independent thermometer with its own alarm covers a failed or drifted probe, a dead
   controller and a dead compressor.

## What the software guarantees, and its limits

Control runs in the application, but the relay lines belong to a separate process, `curing-outputd`.
Any output stays on only while control keeps renewing a 3 s lease (`docs/adr/0207-output-lease.md`).

Measured values for every row below are in `evidence/b/`, from RP1 registers with no relay attached:

| Failure | What turns the lines off | Evidence |
|---|---|---|
| Control loop stuck, process alive | lease expiry | `28-...` |
| Control process frozen | lease expiry | `28-...` |
| Control process dead | socket close | `28-...` |
| Guard frozen | systemd kills it, the kernel stops driving | `28-...` |
| Guard dead | the kernel stops driving | `28-...` |
| `persist_gpio_outputs` wrong or missing | the guard turns all off, refuses on | `29-...` |
| Kernel panic | board reset, 1 s after the panic by configuration | `40-...`, bracket only |
| Kernel hard hang | hardware watchdog reset, 10 s by configuration | `43-...`, a panic that halts stands in for a hang; attribution not proven (ADR 0203) |
| Power loss | supply gone | `static-power-cut-*` |

Limits:
- **A live control loop that commands a wrong output is obeyed.** Refusing to heat with no valid
  temperature is `core`'s decision, not this layer's. The test driver in the captures turns all six
  outputs on to prove this layer's behaviour; a real control loop does not do that.
- **In a kernel panic or hang, relays hold their last state until the reset.** Nothing on the board
  can run then. No instrument on this bench sees when the lines drop in that window; the captures
  give a bracket on the dev host's clock.
- Nothing above helps a welded contact, which is why safety item 1 exists.
- **Firmware sometimes misses the panel on a soft reset.** `curing-hw-init` then loads the panel overlay
  at runtime, proven on 2 blank boots. If that fails, `/run/curing/display-missing` records it and a power
  cycle is the only known remedy (`docs/adr/0209-panel-missing-after-soft-reset.md`).
- **Optional, stronger:** a retriggerable one-shot that gates relay coil power and is kept alive by a
  pulse train from a spare GPIO would cover the panic window in hardware. It is not designed or
  tested here.

## Bring-up checklist

Run this at first power-on, in order, with mains disconnected from every load until step 6
passes. Each step takes a minute or two. Commands run on the dev machine.
A step's result is all you need to record. **Stop at any failure** and fix it before going on.

**0. Before power.** With a meter, Pi off:
- 3.3 V to GND on the header: not shorted.
- Relay module JD-VCC or VCC jumper as the module's data says. Trigger jumper on H.

**1. Base system.** Set `RIG_HOST` to this cabinet's address. `deploy/rig.sh provision-default`, then
`deploy/rig.sh hwcheck safe-state`. Use `provision-default`, not `provision`: the latter installs a bench
test helper that can panic the kernel. Pass: `provision: done`, then
`VerifySafeState is_ErrUnsafeState=false err=<nil>` and `DisplayMissing missing=false`, then
`deploy/rig.sh bench-helper-state` prints `curing-rig-test: absent`.

**2. DHT22.** Power off, wire it, power on. `deploy/rig.sh hwcheck read-sensors`.
Pass, all of: `dht22` lines show `err=<nil>` within about 5 s; temperature within 2 C and humidity
within 10 %RH of a separate instrument nearby; values change when you breathe on it, within a minute.
- `device absent` with `Only 0 signal edges` in `deploy/rig.sh journal 50 all`: wiring or power.
- Mostly-good readings with occasional `bad read`: acceptable if under about 1 in 10. More means a
  long lead or weak pull-up. Try the 4.7 kOhm pull-up. If the kernel driver never works but wiring is
  right, try `DHTGPIO` (`docs/adr/0205-dht22-backend.md`).

**3. FS3000.** Same command. Pass, all of: `fs3000` lines show `err=<nil>`; about 0 m/s (under 0.2)
in still air; the value rises when you blow gently across the sensor and falls back afterwards.
- `device absent`: Qwiic cable or power.
- Constant `bad read` with `checksum`: **the one rule not checked against a real part.** Note the
  frame bytes in the error and compare with the Renesas datasheet. SparkFun's library ignores this
  checksum, so this is what to examine first.

**4. HX711 and load cell.** Same command. Pass, all of: `hx711` lines show `err=<nil>`; with nothing on
the cell, raw varies by well under 1 % of the range between lines; press the cell lightly and raw
moves steadily and returns; add a known weight and the change is repeatable within about 1 % over three
tries.
- `device absent`: DOUT or SCK wiring, or no power.
- `bad read` with `saturated`: bridge wiring open or swapped.
- `bad read` with `clock held high` more than rarely: the board is overloaded. Check `deploy/rig.sh
  hwcheck hx711-timing` for `over_50us`.

**5. Fan.** Wire the buffer and fan. `deploy/rig.sh hwcheck fan-sweep`. It holds each duty for 8 s.
Pass, all of: no `err` other than `<nil>` at 30 % and above; RPM rises with duty; at 100 % about 3000
RPM (+/- 10 %); with a meter on the fan's PWM wire, it is not above 5.5 V, and GPIO 6 is at 3.3 V or
less at the header.
- `device absent` at 30 % or above after 5 s: no tach. Check its pull-up.
- `bad read` with `above anything this fan can do`: PWM crosstalk on tach. Separate the wires.
- Record what duty 0 does. If the fan does not stop at 0 or stalls at 30 %, adjust `tachExpectDuty`.
- Fan running backwards from expected duty: `FanInvert` does not match the buffer.

**6. Relays, no mains.** Wire the module, no loads on its contacts. For each of `cool heat humid
dehumid alarm buzzer`: `deploy/rig.sh hwcheck relay-click <output>`. It energizes one for 3 s.
Pass, all of, for every output:
- Exactly that relay clicks on and off, confirmed with a meter across its contacts.
- With the Pi powered and no software commanding it, every relay is off, and the line reads under
  0.8 V at the module with the module connected. This is the check on the input bias.
- Power the Pi off with the module still powered: every relay stays off. **Any relay on here means
  the module is low-level trigger or wrongly biased. Do not connect it to mains.**

**6a. Alarm contact, no mains.** Wire the contact as under "Alarm contact", with its sounder or dialer on
its own power. Run the application with every sensor connected and in range, no alarms active. Pass, all of:
the relay pulls in within a minute of start and the sounder is quiet; `deploy/rig.sh service-kill KILL`
makes it signal at once, and it returns to quiet about 20 s after the service is back; removing the Pi's
power makes it signal and it keeps signalling. Repeat at each commissioning: a welded contact looks healthy.

**7. Relay fail-safe with the module attached, no mains.** `deploy/rig.sh hwcheck time-to-safe`.
Takes about a minute and energizes all six relays in turn. Pass, all of: every row shows
`still_high=0` and `high_samples_in_next_1500ms=0`; every relay audibly drops in each scenario; times
close to `evidence/b/28-...`, about 3 s for the stuck and frozen rows and 2 s for guard frozen.

**8. Independent cutout, mains on, supervised.** Run the heater from a load test or a control run,
and trigger the cutout by heating its sensor (or per its data sheet). Pass: the heater stops with the
controller still commanding it on. Reset it and check it again. Fit the separate thermometer and alarm.

**9. Mains loads.** Connect one load at a time and repeat step 6's click. Pass: the load switches with
its relay and the control electronics do not reset. A reset or false sensor read when a load switches
means a snubber is missing (safety item 2).

Keep the printed output of steps 2 to 7 with the cabinet's records: it is the first evidence against
real hardware.

## Not tested

Nothing below has been run against real hardware, because the rig has only the display:

- Reading any real DHT22, FS3000 or HX711, or spinning a real P12 Pro.
- The FS3000 checksum rule against a real sensor. SparkFun's Arduino library computes
  this checksum but ignores a mismatch, so real sensors might not always satisfy it.
  This driver rejects those frames. Check it first when one is connected.
- The kernel `dht11` driver with a real DHT22 on the RP1. Without a sensor it returns
  `ETIMEDOUT`, which is all that has been seen.
- `DHTGPIO`, the user-space DHT22 backend, on the rig: fake pulse trains only. It would
  also need the `dht11` overlay removed from `curing-hw-init`.
- Real HX711 timing: only the SCK high time has been measured on the rig, with nothing
  attached.
- Real tach signals, the PWM buffer, the PWM signal at the fan, and the fan's response.
- Real relay modules. The lines' levels were read from RP1 registers, not on a meter.
- The alarm contact on a real relay, either terminal, with any real sounder or dialer. GPIO 24 has only
  been seen going high and low. Real hardware mode with no sensors never reaches healthy. The healthy
  state was reached only with the simulated plant driving it (`--sim-annunciate`), which ADR 0406
  removed because simulated alarms then reached the real contact.
- A real buzzer.
- Long-term drift or noise of anything.
- Door switch input on a real switch.

The fan PWM frequency and duty, relay line levels through each failure, and `ErrAbsent` bounds
have been measured on the rig, from RP1 registers and driver calls, with nothing attached. The bring-up
checklist above closes the list here. None of its steps has been run.
