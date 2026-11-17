package cost

// NormalizeCatalogue uses the same validation as catalogue writes.
func NormalizeCatalogue(in *CatalogueInput) error {
	if err := validate(in); err != nil {
		return err
	}
	return nil
}
func NormalizeDefinitions(in []Definition) error {
	if err := validateDefinitions(in); err != nil {
		return err
	}
	return nil
}
