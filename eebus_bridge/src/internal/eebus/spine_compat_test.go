package eebus

import "github.com/enbility/spine-go/model"

// Heat pumps such as the Bosch CS5800i send partial notifies of their power
// sequences (OHPCF). spine-go only merges them for types implementing
// model.Updater; older versions rejected them with "partial updates are not
// supported for type 'SmartEnergyManagementPsDataType'" (#7).
var _ model.Updater = (*model.SmartEnergyManagementPsDataType)(nil)
