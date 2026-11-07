# ADR 0110: Compressor failure detection and temperature-high severity

Status: accepted

## Context
In round 1 a dead compressor first raised HEAT_STUCK_ON at 4842 s, wrongly blaming the
heater; the correct alarm came at 6840 s and TEMP_HIGH at 11743 s. TEMP_HIGH was warning
severity at any temperature.

## Detection
Options: wait for a temperature band (slow, and it says nothing about the cause); a coil
or current sensor (none exists); judge each run by the air's response.

Decision: judge each run. A healthy run drops the air noticeably within 2 min of stopping.
Four runs in a row with under 0.2 °C of fall and no heater running count as failure, and so
does any 15 min of continuous run with no 0.2 °C fall while above setpoint. The second rule
counts with a heater running, because a heater winning against the compressor is also not
cooling, and the alarm text says so. HEAT_STUCK_ON now needs compressor runs to be visibly
working (last drop at least 0.5 °C), so it can no longer blame the heater for a dead compressor.

A room within a degree or two of setpoint shows no failure, because no cooling is needed.
Those plants stay in the sweep as misses.

## Severity
Options: warning only, as reference controllers (and R4.8 leaves regulation unchanged);
critical at the same band; two levels.

Decision: two levels, both informational to regulation. TEMP_HIGH stays warning at set +
2.5 °C after 15 min. TEMP_HIGH_CRITICAL fires, after the same delay, at any of: set + 5 °C;
over the band and over 15.6 °C with a setpoint under that (the S. aureus reference in
BENCHMARK 7.1, used as a line for urgency and not as a statement about safety); or over the
band for 2 h. The absolute limit stays critical and is never suppressed.

## Consequences
An unattended cabinet whose cooling has failed holds the contact open from the first
critical alarm until someone acknowledges it, and after 12 h signals again.
