//go:build !linux

package transcode

func (*cacheRoot) MaintainPlanJob(string, Plan, int) (cacheMaintenanceResult, error) {
	return cacheMaintenanceResult{}, ErrUnsupported
}

func (*cacheRoot) FinalizePlanJob(string, Plan) (int64, bool, error) {
	return 0, false, ErrUnsupported
}
