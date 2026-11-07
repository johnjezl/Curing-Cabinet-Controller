# Verdict

**No.** I would not leave a batch of meat in this for three weeks unattended, even with real sensors and relays fitted.

## The three reasons that decide it

1. **It does not hold the recipe, and it calls the miss small.** The true plant temperature from the logged samples (log/, computed per phase and setpoint):

   | Phase | Setpoint | True air mean (min-max) | Heater on | True RH mean (min-max) |
   |---|---|---|---|---|
   | Ferment | 24 C / 90 % | 20.7 C (14.2-23.6) | 2 of 62 samples | 93.9 % (53.9-97.7) |
   | Dry early | 14 C / 82 % | 14.27 C (12.4-25.0, faults included) | | 82.6 % |
   | Dry | 13 C / 76 % | 13.49 C (7.1-18.7, faults included) | | 77.2 % |
   | Dry, override | 15 C / 78 % | 13.87 C (11.1-15.8) | | 83.9 % (61-94) |

   In the Ferment phase the heater barely ran while the panel read "HEAT off: slightly under setpoint" at -4.7 C. Ferment humidity sat 4-8 % over setpoint with the dehumidifier never used. A cool, slow ferment with RH that is too high is a food-safety problem, not a cosmetic one.

2. **When the equipment fails, it blames the wrong part and the panel shows the command, not the fact.**
   - A real stuck-on heater produced no cause alarm, and "HEAT off" stayed on screen for about 16 virtual hours at +5.9 C.
   - A healthy heater was twice accused, with "Disconnect it".
   - Under an output write fault the panel showed COOL ON and HEAT ON while nothing could switch, and the buzzer stayed silent through a critical alarm.
   - Stale events took the headline over live critical excursions.

   An operator 2 m away is told the wrong thing exactly when it matters.

3. **The remote alarm channel can be stuck open for the rest of a batch, and the warm-hazard sensor faults are unproven.**
   - After a restart burst, a latched critical held the alarm contact open for the remaining days of the cure. Every later alarm was therefore invisible to a dialer.
   - The drift and stuck sensor injections had no effect at all, so detection of a sensor reading low while the product warms is untested. That is the main way an unattended cure spoils meat.
   - The outage history records crashes as power losses, with unknown durations and wrong start times.

## What must be true before I would say yes

- The heater and dehumidification regulate to within about +/-1 C and +/-5 % RH of every recipe setpoint, demonstrated over a full time-compressed cure with no injected faults. The panel's reason text must match the size of the error.
- A stuck-on heater, a stuck-on humidifier and a compressor that does not cool are each identified correctly within a stated time, with no false "disconnect" on healthy equipment in a hot room. The output chips must show proved state, or say UNKNOWN on a write fault.
- The alarm contact signals every new critical alarm as a fresh transition, or latched system alarms clear or re-arm on their own. The buzzer must sound on a critical alarm even when plant outputs cannot be written.
- The simulator's drift and stuck sensor faults visibly change the reading. With them working, a sensor drifting low by 2-3 C must raise an alarm before the product passes 2.5 C over setpoint for the alarm delay.
- The outage and crash history carries the true loss time, the duration and the cause. Weight loss must be re-based or blanked after a load change.
- Simulation mode carries a full-width SIMULATION/xN banner and does not drive the real alarm contact.
- The whole set of tests above must be re-run on a clean cure without my accidental fault window, with real sensors and relays on the bench.
