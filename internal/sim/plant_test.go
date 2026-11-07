package sim

import (
	"math"
	"testing"

	"github.com/johnjezl/curing-cabinet-controller/internal/hal"
)

// The plant must conserve water; otherwise moisture results mean nothing.
func TestMoistureBalanceCloses(t *testing.T) {
	p := BasePlant()
	p.InfilACH = 0
	pl := NewPlant(p)
	w0 := pl.WAir + pl.Sorb*p.SorbG + pl.ProdWater
	pl.CmdCool = true
	pl.CmdHumid = true
	pl.FanDuty = 40
	pl.fanRunning = true
	for i := 0; i < 86400; i++ {
		pl.Step(1)
	}
	w1 := pl.WAir + pl.Sorb*p.SorbG + pl.ProdWater + pl.Film + pl.Frost + pl.Drained
	in := pl.HumidIn
	// Water pushed past saturation is put into the buffer, which can clip at 1;
	// the check allows only a small amount.
	if d := math.Abs(w1 - (w0 + in)); d > 5 {
		t.Fatalf("water balance off by %.2f g (start %.1f, end %.1f, added %.1f)", d, w0, w1, in)
	}
}

// With everything off, the cabinet must approach the room and never pass it.
func TestPassiveApproachToRoom(t *testing.T) {
	p := BasePlant()
	p.AmbientSwingC = 0
	pl := NewPlant(p)
	pl.SetInitial(5, 80)
	for i := 0; i < 5*86400; i++ {
		pl.Step(1)
		if pl.TAir > p.AmbientC+0.5 {
			t.Fatalf("air passed room temperature: %v", pl.TAir)
		}
	}
	if math.Abs(pl.TAir-p.AmbientC) > 1.5 {
		t.Fatalf("air after 5 days: %v", pl.TAir)
	}
}

func TestPhysicalEstimatesAreInRange(t *testing.T) {
	// Compressor running flat out must reach cold but not absurd temperature, and
	// a 10 kg load must pull down in hours, not minutes.
	p := BasePlant()
	p.AmbientSwingC = 0
	pl := NewPlant(p)
	pl.CmdCool = true
	pl.FanDuty = 30
	for i := 0; i < 3600; i++ {
		pl.Step(1)
	}
	if pl.TAir > 15 || pl.TAir < -5 {
		t.Fatalf("air after 1 h flat out: %v", pl.TAir)
	}
	if pl.TProd < 5 {
		t.Fatalf("product should lag: %v", pl.TProd)
	}
	// Fresh product at cure conditions loses 1 to 3 %/day.
	q := BasePlant()
	q.AmbientSwingC = 0
	pl = NewPlant(q)
	pl.SetInitial(13, 78)
	pl.FanDuty = 30
	pl.fanRunning = true
	for i := 0; i < 86400; i++ {
		pl.Step(1)
		pl.TAir, pl.TWall, pl.TProd = 13, 13, 13
		pl.WAir = 0.78 * vaporDensity(13) * q.VolumeM3
	}
	if l := pl.WaterLossPct(); l < 1 || l > 4 {
		t.Fatalf("loss in the first day at cure conditions: %v %%", l)
	}
}

func TestSensorQuantization(t *testing.T) {
	pl := NewPlant(BasePlant())
	pl.P.DHTErrProb = 0
	pl.P.DHTSpikePerHour = 0
	c := simClimate{pl}
	a, b, age, err := c.Read()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(a*10-math.Round(a*10)) > 1e-6 || math.Abs(b*10-math.Round(b*10)) > 1e-6 {
		t.Fatalf("DHT22 reads in 0.1 steps, got %v %v", a, b)
	}
	if age != 0 {
		t.Fatalf("age %v", age)
	}
	// The sample must hold for 2 s.
	pl.Step(1)
	_, _, age, _ = c.Read()
	if age != 1 {
		t.Fatalf("age after 1 s: %v", age)
	}
	if v := fs3000Quant(9); v != 7.23 {
		t.Fatalf("FS3000 clip %v", v)
	}
}

func TestDeadDevicesReportAbsent(t *testing.T) {
	pl := NewPlant(BasePlant())
	pl.Inject(DevClimate, FaultDead, 0)
	if _, _, _, err := (simClimate{pl}).Read(); !isAbsent(err) {
		t.Fatalf("climate: %v", err)
	}
	pl.Inject(DevAir, FaultDead, 0)
	if _, err := (simAir{pl}).Read(); !isAbsent(err) {
		t.Fatalf("air: %v", err)
	}
	pl.Inject(DevScale, FaultDead, 0)
	if _, err := (simScale{pl}).ReadRaw(); !isAbsent(err) {
		t.Fatalf("scale: %v", err)
	}
}

func isAbsent(err error) bool {
	for e := err; e != nil; {
		if e == hal.ErrAbsent {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

func TestSweepIsReproducibleAndCoversRanges(t *testing.T) {
	a, b := Sweep(40, 1), Sweep(40, 1)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("plant %d differs between identical sweeps", i)
		}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range a[12:] {
		lo = math.Min(lo, p.CompW)
		hi = math.Max(hi, p.CompW)
	}
	if lo > 95 || hi < 195 {
		t.Fatalf("Latin hypercube did not cover CompW: %v..%v", lo, hi)
	}
}
