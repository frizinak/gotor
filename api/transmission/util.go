package transmission

func pvd[X any](val *X, def X) X {
	if val == nil {
		return def
	}

	return *val
}

func pv[X any](val *X) X {
	var def X
	if val == nil {
		return def
	}

	return *val
}
