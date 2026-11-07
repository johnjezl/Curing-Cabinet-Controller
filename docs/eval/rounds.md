# Critic rounds

| Part | Round | Auditor verdict | Blind judge | Largest gap sent to builder |
|---|---|---|---|---|
| B | 1 | would not trust a batch | reference preferred, clear | outputs held on with failed temperature read; no real hardware; no good-to-lost transition |
| A | 1 | defects in metrics and fault response | reference preferred, moderate | alarm relay on 81-94% of cure time; drift undetected; open-loop cooling freezes product |
| A | 2 | new cross-check overrules a healthy sensor with no fault injected; acknowledge silences a live critical | reference preferred, moderate | latched distrust; contact behaviour on fault and acknowledge |
| A | 3 | false alarms gone in fault-free runs; heats on a flagged low-reading sensor (warm hazard); frost runaway; buzzer gaps | reference preferred, moderate | STOPPED: same blocker repeats (no real equipment); final safety fix pass only, no further judging | Fix pass done: suspicion floors cooling (fast low drift peak air 18.5 C median at 13 C set, was 37.6); 4 C frost fixed; new cold regression at 24 C (min air -4.2 C median vs -2.0) left open. |
| B | 2 | outputs self-drop measured (15 of 15); nothing real attached | reference preferred, clear; trust by default, not proof | STOPPED: same blocker repeats (no real sensor or load can be attached to this rig) |
| C | 1 | defects found, notably stalled-controller readings shown as live | ours preferred, moderate; not ready until tested on real hardware | beat the reference at this layer; defect fixes sent, re-checked by integration critic |
| D | 1 | final evidence on settled tree; no blank boots in 9; core false-disagree late in cure and undetected 1 C/h drift seen in sim cure; audit: silence does nothing, wrong outage records, restart loop on write error | ours preferred, narrow | beat the reference; fix pass done (silence, outage record, no clean-restart outage alarm, no write-error restart loop) |
| Integration | 1 | verdict NO: misses ferment setpoint, misnames stuck heater, latched alarm blinds contact, sim drift faults inert | reference preferred, moderate | stopped: final critic, as instructed |
