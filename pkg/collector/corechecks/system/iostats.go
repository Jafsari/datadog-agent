package system

import (
	"bytes"
	"time"

	"github.com/DataDog/datadog-agent/pkg/collector/check"
	core "github.com/DataDog/datadog-agent/pkg/collector/corechecks"
	log "github.com/cihub/seelog"
	"github.com/shirou/gopsutil/disk"

	"github.com/DataDog/datadog-agent/pkg/aggregator"
)

/*
#include <unistd.h>
#include <sys/types.h>
#include <pwd.h>
#include <stdlib.h>
*/
import "C"

// For testing purpose
var ioCounters = disk.IOCounters

// kernel ticks / sec
var hz uint64

const (
	// SectorSize is exported in github.com/shirou/gopsutil/disk (but not working!)
	SectorSize = 512
	kB         = 1024
)

// IOCheck doesn't need additional fields
type IOCheck struct {
	sender aggregator.Sender
}

func (c *IOCheck) String() string {
	return "IOCheck"
}

// Run executes the check
func (c *IOCheck) Run() error {
	iomap, err := ioCounters()
	if err != nil {
		log.Errorf("system.IOCheck: could not retrieve io stats: %s", err)
		return err
	}

	//sleep and collect again
	time.Sleep(time.Second)
	iomap2, err := ioCounters()
	if err != nil {
		log.Errorf("system.IOCheck: could not retrieve io stats: %s", err)
		return err
	}

	for device, ioStats := range iomap {
		ioStats2, ok := iomap2[device]
		if !ok {
			log.Infof("New device stats (possible hotplug) - full stats unavailable this iteration.")
			continue
		}

		var tagbuff bytes.Buffer
		tagbuff.WriteString("device:")
		tagbuff.WriteString(device)

		// TODO: Different OS have different fields.... account for that
		// See: https://www.xaprb.com/blog/2010/01/09/how-linux-iostat-computes-its-results/
		//      https://www.kernel.org/doc/Documentation/iostats.txt
		// Linux
		rrqms := (ioStats2.MergedReadCount - ioStats.MergedReadCount)
		wrqms := (ioStats2.MergedWriteCount - ioStats.MergedWriteCount)
		rs := (ioStats2.ReadCount - ioStats.ReadCount)
		ws := (ioStats2.WriteCount - ioStats.WriteCount)
		rkbs := float64(ioStats2.ReadBytes-ioStats.ReadBytes) / kB
		wkbs := float64(ioStats2.WriteBytes-ioStats.WriteBytes) / kB
		avgqusz := float64(ioStats2.WeightedIO-ioStats.WeightedIO) / 1000

		rAwait := 0.0
		wAwait := 0.0
		diffNRIO := float64(ioStats2.ReadCount - ioStats.ReadCount)
		diffNWIO := float64(ioStats2.WriteCount - ioStats.WriteCount)
		if diffNRIO != 0 {
			rAwait = float64(ioStats2.ReadTime-ioStats.ReadTime) / diffNRIO
		}
		if diffNWIO != 0 {
			wAwait = float64(ioStats2.WriteTime-ioStats.WriteTime) / diffNWIO
		}

		avgrqsz := 0.0
		await := 0.0
		svctime := 0.0

		diffNIO := float64(diffNRIO + diffNWIO)
		if diffNIO != 0 {
			avgrqsz = float64((ioStats2.ReadBytes-ioStats.ReadBytes+ioStats2.WriteBytes-ioStats.WriteBytes)/SectorSize) / diffNIO
			await = float64(ioStats2.ReadTime-ioStats.ReadTime+ioStats2.WriteTime-ioStats.WriteTime) / diffNIO
		}
		tput := diffNIO * float64(hz)
		util := float64(ioStats2.IoTime - ioStats.IoTime)
		if tput != 0 {
			svctime = util / tput
		}

		tags := []string{tagbuff.String()}
		c.sender.Gauge("system.io.rrqm_s", float64(rrqms), "", tags)
		c.sender.Gauge("system.io.wrqm_s", float64(wrqms), "", tags)
		c.sender.Gauge("system.io.r_s", float64(rs), "", tags)
		c.sender.Gauge("system.io.w_s", float64(ws), "", tags)
		c.sender.Gauge("system.io.rkb_s", rkbs, "", tags)
		c.sender.Gauge("system.io.wkb_s", wkbs, "", tags)
		c.sender.Gauge("system.io.avg_rq_sz", avgrqsz, "", tags)
		c.sender.Gauge("system.io.avg_q_sz", avgqusz, "", tags)
		c.sender.Gauge("system.io.await", await, "", tags)
		c.sender.Gauge("system.io.r_await", float64(rAwait), "", tags)
		c.sender.Gauge("system.io.w_await", float64(wAwait), "", tags)
		c.sender.Gauge("system.io.svctm", svctime, "", tags)

		// Stats should be per device no device groups.
		// If device groups ever become a thing - util / 10.0 / n_devs_in_group
		// See more: (https://github.com/sysstat/sysstat/blob/v11.5.6/iostat.c#L1033-L1040)
		c.sender.Gauge("system.io.util", (util / 10.0), "", tags)

		// TODO: enable blacklist
		c.sender.Commit()
	}

	return nil
}

// Configure the CPU check doesn't need configuration
func (c *IOCheck) Configure(data check.ConfigData, initConfig check.ConfigData) error {
	return nil
}

// InitSender initializes a sender
func (c *IOCheck) InitSender() {
	s, err := aggregator.GetSender(c.ID())
	if err != nil {
		log.Error(err)
		return
	}

	c.sender = s
}

// Interval returns the scheduling time for the check
func (c *IOCheck) Interval() time.Duration {
	return check.DefaultCheckInterval
}

// ID returns the name of the check since there should be only one instance running
func (c *IOCheck) ID() check.ID {
	return check.ID(c.String())
}

// Stop does nothing
func (c *IOCheck) Stop() {}

func ioFactory() check.Check {
	return &IOCheck{}
}

func init() {
	core.RegisterCheck("io", ioFactory)

	// get the clock frequency - one time op
	var scClkTck C.long
	scClkTck = C.sysconf(C._SC_CLK_TCK)

	hz = uint64(scClkTck)
}
