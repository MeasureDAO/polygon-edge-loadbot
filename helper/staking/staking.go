package staking

import (
	"encoding/json"
	"fmt"
	"github.com/0xPolygon/polygon-edge/contracts/staking"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/state"
	itrie "github.com/0xPolygon/polygon-edge/state/immutable-trie"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/state/runtime/evm"
	"io/ioutil"
	"math"
	"math/big"
	"os"
	"strings"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/helper/keccak"
	"github.com/0xPolygon/polygon-edge/types"
)

var (
	MinValidatorCount = uint64(1)
	MaxValidatorCount = common.MaxSafeJSInt
)

// getAddressMapping returns the key for the SC storage mapping (address => something)
//
// More information:
// https://docs.soliditylang.org/en/latest/internals/layout_in_storage.html
func getAddressMapping(address types.Address, slot int64) []byte {
	bigSlot := big.NewInt(slot)

	finalSlice := append(
		common.PadLeftOrTrim(address.Bytes(), 32),
		common.PadLeftOrTrim(bigSlot.Bytes(), 32)...,
	)
	keccakValue := keccak.Keccak256(nil, finalSlice)

	return keccakValue
}

// getIndexWithOffset is a helper method for adding an offset to the already found keccak hash
func getIndexWithOffset(keccakHash []byte, offset int64) []byte {
	bigOffset := big.NewInt(offset)
	bigKeccak := big.NewInt(0).SetBytes(keccakHash)

	bigKeccak.Add(bigKeccak, bigOffset)

	return bigKeccak.Bytes()
}

// getStorageIndexes is a helper function for getting the correct indexes
// of the storage slots which need to be modified during bootstrap.
//
// It is SC dependant, and based on the SC located at:
// https://github.com/0xPolygon/staking-contracts/
func getStorageIndexes(address types.Address, index int64) *StorageIndexes {
	storageIndexes := StorageIndexes{}

	// Get the indexes for the mappings
	// The index for the mapping is retrieved with:
	// keccak(address . slot)
	// . stands for concatenation (basically appending the bytes)
	storageIndexes.AddressToIsValidatorIndex = getAddressMapping(address, addressToIsValidatorSlot)
	storageIndexes.AddressToStakedAmountIndex = getAddressMapping(address, addressToStakedAmountSlot)
	storageIndexes.AddressToValidatorIndexIndex = getAddressMapping(address, addressToValidatorIndexSlot)

	// Get the indexes for _validators, _stakedAmount
	// Index for regular types is calculated as just the regular slot
	storageIndexes.StakedAmountIndex = big.NewInt(stakedAmountSlot).Bytes()

	// Index for array types is calculated as keccak(slot) + index
	// The slot for the dynamic arrays that's put in the keccak needs to be in hex form (padded 64 chars)
	storageIndexes.ValidatorsIndex = getIndexWithOffset(
		keccak.Keccak256(nil, common.PadLeftOrTrim(big.NewInt(validatorsSlot).Bytes(), 32)),
		index,
	)

	// For any dynamic array in Solidity, the size of the actual array should be
	// located on slot x
	storageIndexes.ValidatorsArraySizeIndex = []byte{byte(validatorsSlot)}

	return &storageIndexes
}

// PredeployParams contains the values used to predeploy the PoS staking contract
type PredeployParams struct {
	MinValidatorCount uint64
	MaxValidatorCount uint64
}

// StorageIndexes is a wrapper for different storage indexes that
// need to be modified
type StorageIndexes struct {
	ValidatorsIndex              []byte // []address
	ValidatorsArraySizeIndex     []byte // []address size
	AddressToIsValidatorIndex    []byte // mapping(address => bool)
	AddressToStakedAmountIndex   []byte // mapping(address => uint256)
	AddressToValidatorIndexIndex []byte // mapping(address => uint256)
	StakedAmountIndex            []byte // uint256
}

// Slot definitions for SC storage
var (
	validatorsSlot              = int64(0) // Slot 0
	addressToIsValidatorSlot    = int64(1) // Slot 1
	addressToStakedAmountSlot   = int64(2) // Slot 2
	addressToValidatorIndexSlot = int64(3) // Slot 3
	stakedAmountSlot            = int64(4) // Slot 4
	minNumValidatorSlot         = int64(5) // Slot 5
	maxNumValidatorSlot         = int64(6) // Slot 6
)

const (
	DefaultStakedBalance = "0x8AC7230489E80000" // 10 ETH
	//nolint: lll
	StakingSCBytecode = "0x6080604052600436106100f75760003560e01c80637dceceb81161008a578063e387a7ed11610059578063e387a7ed14610381578063e804fbf6146103ac578063f90ecacc146103d7578063facd743b1461041457610165565b80637dceceb8146102c3578063af6da36e14610300578063c795c0771461032b578063ca1e78191461035657610165565b8063373d6132116100c6578063373d6132146102385780633a4b66f114610263578063714ff4251461026d5780637a6eea371461029857610165565b806302b751991461016a578063065ae171146101a75780632367f6b5146101e45780632def66201461022157610165565b366101655761011b3373ffffffffffffffffffffffffffffffffffffffff16610451565b1561015b576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610152906111c0565b60405180910390fd5b610163610464565b005b600080fd5b34801561017657600080fd5b50610191600480360381019061018c9190610f3e565b61062e565b60405161019e919061121b565b60405180910390f35b3480156101b357600080fd5b506101ce60048036038101906101c99190610f3e565b610646565b6040516101db9190611145565b60405180910390f35b3480156101f057600080fd5b5061020b60048036038101906102069190610f3e565b610666565b604051610218919061121b565b60405180910390f35b34801561022d57600080fd5b506102366106af565b005b34801561024457600080fd5b5061024d61079a565b60405161025a919061121b565b60405180910390f35b61026b6107a4565b005b34801561027957600080fd5b5061028261080d565b60405161028f919061121b565b60405180910390f35b3480156102a457600080fd5b506102ad610817565b6040516102ba9190611200565b60405180910390f35b3480156102cf57600080fd5b506102ea60048036038101906102e59190610f3e565b610823565b6040516102f7919061121b565b60405180910390f35b34801561030c57600080fd5b5061031561083b565b604051610322919061121b565b60405180910390f35b34801561033757600080fd5b50610340610841565b60405161034d919061121b565b60405180910390f35b34801561036257600080fd5b5061036b610847565b6040516103789190611123565b60405180910390f35b34801561038d57600080fd5b506103966108d5565b6040516103a3919061121b565b60405180910390f35b3480156103b857600080fd5b506103c16108db565b6040516103ce919061121b565b60405180910390f35b3480156103e357600080fd5b506103fe60048036038101906103f99190610f6b565b6108e5565b60405161040b9190611108565b60405180910390f35b34801561042057600080fd5b5061043b60048036038101906104369190610f3e565b610924565b6040516104489190611145565b60405180910390f35b600080823b905060008111915050919050565b600654600080549050106104ad576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016104a490611180565b60405180910390fd5b34600460008282546104bf9190611280565b9250508190555034600260003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008282546105159190611280565b92505081905550600160003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060009054906101000a900460ff161580156105cf5750670de0b6b3a76400006fffffffffffffffffffffffffffffffff16600260003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410155b156105de576105dd3361097a565b5b3373ffffffffffffffffffffffffffffffffffffffff167f9e71bc8eea02a63969f509818f2dafb9254532904319f9dbda79b67bd34a5f3d34604051610624919061121b565b60405180910390a2565b60036020528060005260406000206000915090505481565b60016020528060005260406000206000915054906101000a900460ff1681565b6000600260008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020549050919050565b6106ce3373ffffffffffffffffffffffffffffffffffffffff16610451565b1561070e576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610705906111c0565b60405180910390fd5b6000600260003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205411610790576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161078790611160565b60405180910390fd5b610798610a80565b565b6000600454905090565b6107c33373ffffffffffffffffffffffffffffffffffffffff16610451565b15610803576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016107fa906111c0565b60405180910390fd5b61080b610464565b565b6000600554905090565b670de0b6b3a764000081565b60026020528060005260406000206000915090505481565b60065481565b60055481565b606060008054806020026020016040519081016040528092919081815260200182805480156108cb57602002820191906000526020600020905b8160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019060010190808311610881575b5050505050905090565b60045481565b6000600654905090565b600081815481106108f557600080fd5b906000526020600020016000915054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b6000600160008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060009054906101000a900460ff169050919050565b60018060008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060006101000a81548160ff021916908315150217905550600080549050600360008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055506000819080600181540180825580915050600190039060005260206000200160009091909190916101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555050565b60055460008054905011610ac9576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610ac0906111e0565b60405180910390fd5b6000600260003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020549050600160003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060009054906101000a900460ff1615610b6957610b6833610c5f565b5b6000600260003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508060046000828254610bc091906112d6565b925050819055503373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f19350505050158015610c0d573d6000803e3d6000fd5b503373ffffffffffffffffffffffffffffffffffffffff167f0f5bb82176feb1b5e747e28471aa92156a04d9f3ab9f45f28e2d704232b93f7582604051610c54919061121b565b60405180910390a250565b600080549050600360008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410610ce5576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610cdc906111a0565b60405180910390fd5b6000600360008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054905060006001600080549050610d3d91906112d6565b9050808214610e2b576000808281548110610d5b57610d5a6113cc565b5b9060005260206000200160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1690508060008481548110610d9d57610d9c6113cc565b5b9060005260206000200160006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555082600360008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002081905550505b6000600160008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060006101000a81548160ff0219169083151502179055506000600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055506000805480610eda57610ed961139d565b5b6001900381819060005260206000200160006101000a81549073ffffffffffffffffffffffffffffffffffffffff02191690559055505050565b600081359050610f2381611519565b92915050565b600081359050610f3881611530565b92915050565b600060208284031215610f5457610f536113fb565b5b6000610f6284828501610f14565b91505092915050565b600060208284031215610f8157610f806113fb565b5b6000610f8f84828501610f29565b91505092915050565b6000610fa48383610fb0565b60208301905092915050565b610fb98161130a565b82525050565b610fc88161130a565b82525050565b6000610fd982611246565b610fe3818561125e565b9350610fee83611236565b8060005b8381101561101f5781516110068882610f98565b975061101183611251565b925050600181019050610ff2565b5085935050505092915050565b6110358161131c565b82525050565b6000611048601d8361126f565b915061105382611400565b602082019050919050565b600061106b60278361126f565b915061107682611429565b604082019050919050565b600061108e60128361126f565b915061109982611478565b602082019050919050565b60006110b1601a8361126f565b91506110bc826114a1565b602082019050919050565b60006110d460408361126f565b91506110df826114ca565b604082019050919050565b6110f381611328565b82525050565b61110281611364565b82525050565b600060208201905061111d6000830184610fbf565b92915050565b6000602082019050818103600083015261113d8184610fce565b905092915050565b600060208201905061115a600083018461102c565b92915050565b600060208201905081810360008301526111798161103b565b9050919050565b600060208201905081810360008301526111998161105e565b9050919050565b600060208201905081810360008301526111b981611081565b9050919050565b600060208201905081810360008301526111d9816110a4565b9050919050565b600060208201905081810360008301526111f9816110c7565b9050919050565b600060208201905061121560008301846110ea565b92915050565b600060208201905061123060008301846110f9565b92915050565b6000819050602082019050919050565b600081519050919050565b6000602082019050919050565b600082825260208201905092915050565b600082825260208201905092915050565b600061128b82611364565b915061129683611364565b9250827fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff038211156112cb576112ca61136e565b5b828201905092915050565b60006112e182611364565b91506112ec83611364565b9250828210156112ff576112fe61136e565b5b828203905092915050565b600061131582611344565b9050919050565b60008115159050919050565b60006fffffffffffffffffffffffffffffffff82169050919050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000819050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b7f4e487b7100000000000000000000000000000000000000000000000000000000600052603160045260246000fd5b7f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b600080fd5b7f4f6e6c79207374616b65722063616e2063616c6c2066756e6374696f6e000000600082015250565b7f56616c696461746f72207365742068617320726561636865642066756c6c206360008201527f6170616369747900000000000000000000000000000000000000000000000000602082015250565b7f696e646578206f7574206f662072616e67650000000000000000000000000000600082015250565b7f4f6e6c7920454f412063616e2063616c6c2066756e6374696f6e000000000000600082015250565b7f56616c696461746f72732063616e2774206265206c657373207468616e20746860008201527f65206d696e696d756d2072657175697265642076616c696461746f72206e756d602082015250565b6115228161130a565b811461152d57600080fd5b50565b61153981611364565b811461154457600080fd5b5056fea2646970667358221220531eae5ca3c156b3603476dddb612597c51d357508806fc00ae15908bd887e1264736f6c63430008070033"
)

type ContractArtifact struct {
	ABI              string
	DeployedBytecode string
}

func GenerateContractArtifactFromFile(
	filepath string,
	constructorParams []interface{},
) (*chain.GenesisAccount, error) {
	// Set the code for the staking smart contract
	// Code retrieved from https://github.com/0xPolygon/staking-contracts
	var result map[string]interface{}

	contractABIFile, err := os.Open(filepath)
	if err != nil {
		panic("bad")
	}

	fileContent, err := ioutil.ReadAll(contractABIFile)
	if err != nil {
		panic("bad read")
	}

	err = json.Unmarshal(fileContent, &result)
	if err != nil {
		panic("unmarshal bad")
	}

	//	fetch abi
	//abiRaw, ok := result["abi"]
	//if !ok {
	//	panic("bad")
	//}

	//contractAbi, err := json.Marshal(abiRaw)
	//if err != nil {
	//	panic("bad marshal")
	//}

	//	fetch bytecode
	deployedBytecode, ok := result["bytecode"].(string)
	if !ok {
		panic("bad")
	}

	realBytecode, ok := result["deployedBytecode"].(string)
	if !ok {
		panic("bad ")
	}

	//contractArticaft := &ContractArtifact{
	//	ABI:              string(contractAbi),
	//	DeployedBytecode: deployedBytecode,
	//}

	//contractABI, abiErr := abi.NewABI(contractArticaft.ABI)
	//if abiErr != nil {
	//	panic("bad")
	//}

	//constructorArgs, err := abi.Encode(
	//	constructorParams,
	//	contractABI.Constructor.Inputs,
	//)
	//if err != nil {
	//	panic("bad")
	//}

	scHex, err := hex.DecodeString(
		strings.TrimPrefix(deployedBytecode, "0x"),
	)
	if err != nil {
		panic("bad decode bad")
	}

	//finalBytecode := append(scHex, constructorArgs...)

	// 	create state
	st := itrie.NewState(itrie.NewMemoryStorage())

	//	create snapshot
	snapshot := st.NewSnapshot()

	//	create radix
	radix := state.NewTxn(st, snapshot)

	//	create Contract
	contract := runtime.NewContractCreation(
		1,
		types.ZeroAddress,
		types.ZeroAddress,
		staking.AddrStakingContract,
		big.NewInt(0),
		math.MaxInt64,
		scHex,
	)

	config := chain.ForksInTime{
		Homestead:      true,
		Byzantium:      true,
		Constantinople: true,
		Petersburg:     true,
		Istanbul:       true,
		EIP150:         true,
		EIP158:         true,
		EIP155:         true,
	}

	//	create transition (of all above)
	transition := state.NewTransition(config, radix)

	//	run the transition
	res := evm.NewEVM().Run(contract, transition, &config)
	if res.Err != nil {
		panic("bad - evm failed")
	}

	//	walk the state and collect
	storageMap := make(map[types.Hash]types.Hash)
	radix.GetRadix().Root().Walk(func(k []byte, v interface{}) bool {
		addr := types.BytesToAddress(k)
		if addr != staking.AddrStakingContract {
			return false
		}

		obj := v.(*state.StateObject)
		obj.Txn.Root().Walk(func(k []byte, v interface{}) bool {
			storageMap[types.BytesToHash(k)] = types.BytesToHash(v.([]byte))
			println("value", string(v.([]byte)))

			return false
		})

		return true
	})

	transition.Commit()

	realHexBytecode, err := hex.DecodeString(strings.TrimPrefix(realBytecode, "0x"))
	if err != nil {
		panic("bad hex real bytecode")
	}

	stakingAccount := &chain.GenesisAccount{
		Balance: transition.GetBalance(staking.AddrStakingContract),
		Nonce:   transition.GetNonce(staking.AddrStakingContract),
		Code:    realHexBytecode,
		Storage: storageMap,
	}

	return stakingAccount, nil
}

// PredeployStakingSC is a helper method for setting up the staking smart contract account,
// using the passed in validators as pre-staked validators
func PredeployStakingSC(
	validators []types.Address,
	params PredeployParams,
) (*chain.GenesisAccount, error) {
	// Set the code for the staking smart contract
	// Code retrieved from https://github.com/0xPolygon/staking-contracts
	scHex, _ := hex.DecodeHex(StakingSCBytecode)
	stakingAccount := &chain.GenesisAccount{
		Code: scHex,
	}

	// Parse the default staked balance value into *big.Int
	val := DefaultStakedBalance
	bigDefaultStakedBalance, err := types.ParseUint256orHex(&val)

	if err != nil {
		return nil, fmt.Errorf("unable to generate DefaultStatkedBalance, %w", err)
	}

	// Generate the empty account storage map
	storageMap := make(map[types.Hash]types.Hash)
	bigTrueValue := big.NewInt(1)
	stakedAmount := big.NewInt(0)
	bigMinNumValidators := big.NewInt(int64(params.MinValidatorCount))
	bigMaxNumValidators := big.NewInt(int64(params.MaxValidatorCount))

	for indx, validator := range validators {
		// Update the total staked amount
		stakedAmount.Add(stakedAmount, bigDefaultStakedBalance)

		// Get the storage indexes
		storageIndexes := getStorageIndexes(validator, int64(indx))

		// Set the value for the validators array
		storageMap[types.BytesToHash(storageIndexes.ValidatorsIndex)] =
			types.BytesToHash(
				validator.Bytes(),
			)

		// Set the value for the address -> validator array index mapping
		storageMap[types.BytesToHash(storageIndexes.AddressToIsValidatorIndex)] =
			types.BytesToHash(bigTrueValue.Bytes())

		// Set the value for the address -> staked amount mapping
		storageMap[types.BytesToHash(storageIndexes.AddressToStakedAmountIndex)] =
			types.StringToHash(hex.EncodeBig(bigDefaultStakedBalance))

		// Set the value for the address -> validator index mapping
		storageMap[types.BytesToHash(storageIndexes.AddressToValidatorIndexIndex)] =
			types.StringToHash(hex.EncodeUint64(uint64(indx)))

		// Set the value for the total staked amount
		storageMap[types.BytesToHash(storageIndexes.StakedAmountIndex)] =
			types.BytesToHash(stakedAmount.Bytes())

		// Set the value for the size of the validators array
		storageMap[types.BytesToHash(storageIndexes.ValidatorsArraySizeIndex)] =
			types.StringToHash(hex.EncodeUint64(uint64(indx + 1)))
	}

	// Set the value for the minimum number of validators
	storageMap[types.BytesToHash(big.NewInt(minNumValidatorSlot).Bytes())] =
		types.BytesToHash(bigMinNumValidators.Bytes())

	// Set the value for the maximum number of validators
	storageMap[types.BytesToHash(big.NewInt(maxNumValidatorSlot).Bytes())] =
		types.BytesToHash(bigMaxNumValidators.Bytes())

	// Save the storage map
	stakingAccount.Storage = storageMap

	// Set the Staking SC balance to numValidators * defaultStakedBalance
	stakingAccount.Balance = stakedAmount

	return stakingAccount, nil
}
