package simulation

import (
	"yssim-go/app/DataBase"
	"yssim-go/config"
)

// GetExperimentRecord 查询仿真实验记录。
func GetExperimentRecord(experimentID string) (DataBase.YssimExperimentRecord, error) {
	var experiment DataBase.YssimExperimentRecord
	if err := config.DB.Where("id = ?", experimentID).First(&experiment).Error; err != nil {
		return DataBase.YssimExperimentRecord{}, err
	}
	return experiment, nil
}
