package pool

type Status int

const StatusHealthy Status = 0   //a healthy server
const StatusDraining Status = 1  //excluded from round robin
const StatusUnhealthy Status = 2 //removed from the ring

func (condition Status) String() string {
	switch condition {
	case StatusHealthy:
		return "healthy"
	case StatusDraining:
		return "draining"
	case StatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}
