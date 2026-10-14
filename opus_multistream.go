// Ported from libopus 1.6.1 src/opus_multistream.c and the ChannelLayout
// type of src/opus_private.h: helpers shared by the multistream encoder
// and decoder.

package opus

// channelLayout ports ChannelLayout.
type channelLayout struct {
	nbChannels       int
	nbStreams        int
	nbCoupledStreams int
	mapping          [256]byte
}

// validateLayout ports validate_layout.
func validateLayout(layout *channelLayout) bool {
	maxChannel := layout.nbStreams + layout.nbCoupledStreams
	if maxChannel > 255 {
		return false
	}
	for i := 0; i < layout.nbChannels; i++ {
		if int(layout.mapping[i]) >= maxChannel && layout.mapping[i] != 255 {
			return false
		}
	}
	return true
}

// getLeftChannel ports get_left_channel: the next output channel after
// prev (-1 to start) that carries the left channel of coupled stream
// streamID, or -1.
func getLeftChannel(layout *channelLayout, streamID, prev int) int {
	i := 0
	if prev >= 0 {
		i = prev + 1
	}
	for ; i < layout.nbChannels; i++ {
		if int(layout.mapping[i]) == streamID*2 {
			return i
		}
	}
	return -1
}

// getRightChannel ports get_right_channel.
func getRightChannel(layout *channelLayout, streamID, prev int) int {
	i := 0
	if prev >= 0 {
		i = prev + 1
	}
	for ; i < layout.nbChannels; i++ {
		if int(layout.mapping[i]) == streamID*2+1 {
			return i
		}
	}
	return -1
}

// getMonoChannel ports get_mono_channel.
func getMonoChannel(layout *channelLayout, streamID, prev int) int {
	i := 0
	if prev >= 0 {
		i = prev + 1
	}
	for ; i < layout.nbChannels; i++ {
		if int(layout.mapping[i]) == streamID+layout.nbCoupledStreams {
			return i
		}
	}
	return -1
}

// validMSArgs checks encoder stream counts, including all input channels.
func validMSArgs(channels, streams, coupledStreams int) bool {
	return validStreamCounts(channels, streams, coupledStreams) &&
		streams+coupledStreams <= channels
}

// validStreamCounts permits decoder layouts that omit encoded channels.
func validStreamCounts(channels, streams, coupledStreams int) bool {
	return channels <= 255 && channels >= 1 && coupledStreams <= streams &&
		streams >= 1 && coupledStreams >= 0 && streams <= 255-coupledStreams
}
