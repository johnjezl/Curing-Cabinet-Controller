# ADR 0310: What came from fixtures, and what the real core does

Status: accepted

## Context
A critic found screens with all readings bad, an amber banner, an empty alarm list and
COOL ON, and asked what control was doing. Those states came from fixtures written
before the core existed. They show how the panel draws a state, not that the state
can happen.

## Options
1. Keep the fixtures and trust the core to alarm. Leaves unexplained screens, and the
   panel silent if the core ever does not alarm.
2. Delete those fixtures. Loses the defence.
3. Drive the real core on the simulated plant, record what it does, make fixtures match
   it, and keep one labelled defence for what it was never seen to do.

## Decision
Option 3. `internal/ui/simtest` injects sensor faults into the real core and writes its
raw state to evidence/c/13-realcore-states.tsv. Seen there:
- A dead temperature and humidity sensor raises a critical alarm within about 30 s, with
  the core's own text saying heater and humidifier are off and cooling repeats a pattern.
  The core lists those as holds with no time left. So a long-bad reading with no alarm
  is not something the real core was seen to do.
- A stuck sensor reads ok until the core's stuck test trips: between 10 and 20 min for
  temperature and between 20 and 50 min for humidity, in that run's sample points. The
  panel cannot tell during that window, and shows it as live.
- A readings fault marks quality fault while keeping a last value.

The fixtures now match: temp-fault-core-alarm and all-bad carry the core's alarms and
holds. no-alarm-long-fault is the labelled defence: a temperature or humidity reading
unusable for 5 min with no alarm turns the banner critical and says the controller
raised no alarm.

The panel adds what the core does not say on Home: a NO TEMP FEEDBACK line and a marked
COOL ON ! chip, so an output running without a reading does not look like regulation.
A hold with no time left reads as a state and not as a 0:00 countdown.

The alarm path (silence, acknowledge, clear, history, end of cure) runs through the
panel's own targets against the real core, in simtest and on the board. Banner text
wraps and shrinks to whatever the core supplies; a test reads the core's alarm table
and another uses longer text than any it has now.

Evidence hash lists now include internal/core and internal/api, so a panel result
cannot appear to match a core that has moved on.

## Consequences
The simulated plant is not a cabinet. These runs show the core and panel together
against our model, not against real sensors. Fixture trends and weights were also made
consistent, with a test: they used to contradict the loss figures beside them.
