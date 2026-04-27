package beacon

import (
	"context"
	"fmt"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// buildInfo constructs the APRS info field for b, including optional
// comment_cmd stdout appended to the static comment. Lives in its own
// file so scheduler.go can stay focused on the run-loop mechanics;
// nothing in here touches scheduler state beyond the GPS cache and the
// version string used for {{version}} expansion.
func (s *Scheduler) buildInfo(ctx context.Context, b Config) (string, error) {
	comment := ExpandComment(b.Comment, s.version)
	if len(b.CommentCmd) > 0 {
		out, err := RunCommentCmd(ctx, b.CommentCmd, 5*time.Second)
		if err != nil {
			s.logger.Warn("comment_cmd failed", "id", b.ID, "err", err)
			// Fall through with static comment.
		} else if out != "" {
			if comment != "" {
				comment = comment + " " + out
			} else {
				comment = out
			}
		}
	}

	// Pre-encode PHG once. Empty string means "no PHG extension".
	phg := ""
	if b.PHGPower > 0 {
		if encoded, err := aprs.EncodePHG(b.PHGPower, b.PHGHeightFt, b.PHGGainDB, b.PHGDirectivity); err == nil {
			phg = encoded
		} else {
			s.logger.Warn("invalid PHG config", "id", b.ID, "err", err)
		}
	}

	switch b.Type {
	case TypePosition, TypeIGate:
		lat, lon, altM := b.Lat, b.Lon, b.AltFt/3.28084
		if b.UseGps {
			if s.cache == nil {
				return "", fmt.Errorf("%s beacon: use_gps set but no GPS cache configured", b.Type)
			}
			fix, ok := s.cache.Get()
			if !ok {
				return "", fmt.Errorf("%s beacon: use_gps set but no GPS fix available", b.Type)
			}
			lat, lon = fix.Latitude, fix.Longitude
			if fix.HasAlt {
				altM = fix.Altitude
			} else {
				// Never mix GPS lat/lon with a stale fixed AltFt.
				altM = 0
			}
		} else if lat == 0 && lon == 0 {
			return "", fmt.Errorf("%s beacon: fixed coordinates are 0/0 (configure lat/lon or enable use_gps)", b.Type)
		}
		if b.Compress {
			return CompressedPositionInfo(lat, lon, 0, 0, altM, b.SymbolTable, b.SymbolCode, b.Messaging, phg, comment), nil
		}
		return PositionInfo(lat, lon, 0, 0, altM, b.SymbolTable, b.SymbolCode, b.Messaging, phg, comment), nil

	case TypeTracker:
		if s.cache == nil {
			return "", fmt.Errorf("tracker beacon without GPS cache")
		}
		fix, ok := s.cache.Get()
		if !ok {
			return "", fmt.Errorf("tracker beacon: no GPS fix available")
		}
		course := 0
		if fix.HasCourse {
			course = int(fix.Heading)
			if course == 0 {
				course = 360 // APRS encodes 0 as 360 per spec
			}
		}
		altM := 0.0
		if fix.HasAlt {
			altM = fix.Altitude
		}
		// Trackers never emit PHG — CSE/SPD occupies the same slot.
		if b.Compress {
			return CompressedPositionInfo(fix.Latitude, fix.Longitude, course, fix.Speed, altM, b.SymbolTable, b.SymbolCode, b.Messaging, "", comment), nil
		}
		return PositionInfo(fix.Latitude, fix.Longitude, course, fix.Speed, altM, b.SymbolTable, b.SymbolCode, b.Messaging, "", comment), nil

	case TypeObject:
		if b.ObjectName == "" {
			return "", fmt.Errorf("object beacon missing object_name")
		}
		return ObjectInfo(b.ObjectName, true, "", b.Lat, b.Lon, b.SymbolTable, b.SymbolCode, phg, comment), nil

	case TypeCustom:
		if b.CustomInfo == "" {
			return "", fmt.Errorf("custom beacon missing info field")
		}
		if comment != "" {
			return b.CustomInfo + comment, nil
		}
		return b.CustomInfo, nil
	}
	return "", fmt.Errorf("unknown beacon type %q", b.Type)
}

// timeToNextSlot returns the duration until the next occurrence of the
// given "seconds past the hour" boundary.
func timeToNextSlot(now time.Time, slot int) time.Duration {
	if slot < 0 {
		return 0
	}
	slot = slot % 3600
	sec := now.Minute()*60 + now.Second()
	diff := slot - sec
	if diff <= 0 {
		diff += 3600
	}
	return time.Duration(diff)*time.Second - time.Duration(now.Nanosecond())
}
